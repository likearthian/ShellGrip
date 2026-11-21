package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
)

const sshUploadDir = "/tmp/shellgrip-uploads"

// remoteUploadFS is the write subset of an SFTP client used to upload
// dropped files and pasted images to a remote SSH host.
type remoteUploadFS interface {
	MkdirAll(string) error
	CreateUpload(string) (io.WriteCloser, error)
}

type sshUploadItem struct {
	localPath string // local file or directory on disk
	name      string // remote filename when uploading in-memory data
	data      []byte // in-memory payload (e.g., pasted clipboard image)
}

// sshDialUpload is overridden in tests to avoid spawning real ssh processes.
var sshDialUpload = dialSFTPContext

// terminalDrop intercepts local file drops on SSH panes, uploads them over
// SFTP, and pastes their remote paths into the terminal.
func (a *App) terminalDrop(p *Pane, paths []string) bool {
	if p == nil || p.profile.SSH == nil || p.term == nil || len(paths) == 0 {
		return false
	}
	items := make([]sshUploadItem, 0, len(paths))
	for _, local := range paths {
		if local != "" {
			items = append(items, sshUploadItem{localPath: local})
		}
	}
	if len(items) == 0 {
		return false
	}
	a.startSSHUpload(p, items)
	return true
}

// terminalPaste intercepts clipboard pastes containing files or images on SSH
// panes, uploads them over SFTP, and pastes their remote paths into the
// terminal. Plain text pastes return false so the terminal handles them as
// usual.
func (a *App) terminalPaste(p *Pane, d transfer.Data) bool {
	if p == nil || p.profile.SSH == nil || p.term == nil {
		return false
	}
	if files, err := d.Files(); err == nil && len(files) > 0 {
		return a.terminalDrop(p, files)
	}
	// If plain text is present, let the terminal paste it normally.
	if _, hasText := d.Preferred(transfer.Text); hasText {
		return false
	}
	format, ok := d.Preferred(
		transfer.Format("image/png"),
		transfer.Format("image/jpeg"),
		transfer.Format("image/webp"),
		transfer.Format("image/gif"),
	)
	if !ok {
		return false
	}
	data, err := d.Read(format)
	if err != nil || len(data) == 0 {
		return false
	}
	ext := sshImageExt(format)
	name := fmt.Sprintf("paste-%s%s", time.Now().Format("20060102-150405"), ext)
	a.startSSHUpload(p, []sshUploadItem{{name: name, data: data}})
	return true
}

func sshImageExt(format transfer.Format) string {
	switch format {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func (a *App) startSSHUpload(p *Pane, items []sshUploadItem) {
	conn := *p.profile.SSH
	s := a.sftpFor(p)
	if s.watchCtx == nil || s.watchCtx.Err() != nil {
		s.watchCtx, s.watchCancel = context.WithCancel(context.Background())
	}
	existingFS := s.fs
	password := s.password
	parent := s.watchCtx

	go func() {
		fs := existingFS
		var closeFS func()
		dialed := false
		if fs == nil {
			dialCtx, cancel := context.WithTimeout(parent, 30*time.Second)
			var err error
			fs, closeFS, err = sshDialUpload(dialCtx, conn, password)
			cancel()
			if err != nil {
				if parent.Err() != nil {
					return
				}
				a.sshUploadError(s, false, err)
				return
			}
			dialed = true
		}

		uploader, ok := fs.(remoteUploadFS)
		if !ok {
			if closeFS != nil {
				closeFS()
			}
			a.sshUploadError(s, false, errors.New("SFTP session does not support uploads"))
			return
		}

		remotePaths, err := uploadSSHItems(uploader, sshUploadDir, items)
		if err != nil {
			if closeFS != nil {
				closeFS()
			}
			if parent.Err() != nil {
				return
			}
			a.sshUploadError(s, !dialed, err)
			return
		}

		if parent.Err() != nil {
			if closeFS != nil {
				closeFS()
			}
			return
		}

		pasted := joinPOSIXPaths(remotePaths)
		a.onUI(func() {
			if dialed {
				if s.fs == nil && !s.closed {
					s.fs, s.closeFS = fs, closeFS
					s.err, s.needPassword = "", false
					s.notify()
				} else if closeFS != nil {
					go closeFS()
				}
			}
			if !p.closed && p.term != nil && pasted != "" {
				p.term.Paste(pasted)
			}
		})
	}()
}

func (a *App) sshUploadError(s *sftpSession, fromExisting bool, err error) {
	log.Printf("ssh upload: %v", err)
	a.onUI(func() {
		if fromExisting && s != nil && sftpConnLost(err) {
			s.drop(err.Error())
		}
		a.err = "SSH upload failed: " + err.Error()
	})
}

func uploadSSHItems(fs remoteUploadFS, dir string, items []sshUploadItem) ([]string, error) {
	if err := fs.MkdirAll(dir); err != nil {
		return nil, err
	}
	var out []string
	for _, item := range items {
		if item.localPath != "" {
			remote, err := uploadLocalPath(fs, dir, item.localPath)
			if err != nil {
				return nil, err
			}
			out = append(out, remote)
			continue
		}
		remote, err := uploadBytes(fs, dir, item.name, item.data)
		if err != nil {
			return nil, err
		}
		out = append(out, remote)
	}
	return out, nil
}

func uploadLocalPath(fs remoteUploadFS, remoteDir, localPath string) (string, error) {
	info, err := os.Stat(localPath)
	if err != nil {
		return "", err
	}
	base := filepath.Base(localPath)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "upload"
	}
	if !info.IsDir() {
		f, err := os.Open(localPath)
		if err != nil {
			return "", err
		}
		defer f.Close()
		return uploadReader(fs, remoteDir, base, f)
	}

	targetDir := path.Join(remoteDir, base)
	if err := fs.MkdirAll(targetDir); err != nil {
		return "", err
	}
	err = filepath.WalkDir(localPath, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(localPath, p)
		if err != nil || rel == "." {
			return err
		}
		remotePath := path.Join(targetDir, filepath.ToSlash(rel))
		if d.IsDir() {
			return fs.MkdirAll(remotePath)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = uploadReader(fs, path.Dir(remotePath), path.Base(remotePath), f)
		return err
	})
	if err != nil {
		return "", err
	}
	return targetDir, nil
}

func uploadBytes(fs remoteUploadFS, remoteDir, name string, data []byte) (string, error) {
	return uploadReader(fs, remoteDir, name, bytes.NewReader(data))
}

func uploadReader(fs remoteUploadFS, remoteDir, name string, r io.Reader) (string, error) {
	w, remotePath, err := createUniqueRemoteFile(fs, remoteDir, name)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(w, r); err != nil {
		w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return remotePath, nil
}

func createUniqueRemoteFile(fs remoteUploadFS, remoteDir, name string) (io.WriteCloser, string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	if stem == "" {
		stem = "upload"
	}
	for i := 1; i <= 1000; i++ {
		candidate := name
		if i > 1 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		remotePath := path.Join(remoteDir, candidate)
		w, err := fs.CreateUpload(remotePath)
		if err == nil {
			return w, remotePath, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", fmt.Errorf("could not allocate unique name for %s", name)
}

func joinPOSIXPaths(paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuotePOSIX(p)
	}
	return strings.Join(quoted, " ")
}

func shellQuotePOSIX(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '/' || c == '.' || c == '_' || c == '-' || c == '+' || c == ':' || c == '@') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
