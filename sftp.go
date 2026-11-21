package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/sftp"
)

// remoteFS is what the remote tree asks of a directory: the part of
// *sftp.Client it uses, and what tests fake.
type remoteFS interface {
	ReadDir(string) ([]os.FileInfo, error)
	Open(string) (io.ReadCloser, error)
	Getwd() (string, error)
	Close() error
}

// sftpRemote adapts a client to remoteFS: the client's Open returns its
// own file type, which the tree reads as any reader.
type sftpRemote struct{ *sftp.Client }

func (f sftpRemote) Open(name string) (io.ReadCloser, error) { return f.Client.Open(name) }
func (f sftpRemote) CreateRelay(name string) (io.WriteCloser, error) {
	return f.Client.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}
func (f sftpRemote) CreateUpload(name string) (io.WriteCloser, error) {
	return f.Client.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
}
func (f sftpRemote) RenameRelay(old, new string) error { return f.Client.PosixRename(old, new) }

// errSSHAuth tells that the host would not take the keys, and asks for a
// password.
var errSSHAuth = errors.New("the host asks for a password")

// dialSFTP asks the host for its sftp subsystem over ssh, whose stdio the
// SFTP protocol then runs on: ssh keeps its own authentication, agent,
// config and ProxyJump, and the tree sees a real remote filesystem
// rather than the output of ls.
func dialSFTP(c sshConnection, password string) (remoteFS, func(), error) {
	return dialSFTPContext(context.Background(), c, password)
}

func dialSFTPContext(ctx context.Context, c sshConnection, password string) (remoteFS, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	extra := []string{
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "NumberOfPasswordPrompts=1",
		"-o", "BatchMode=no",
	}
	args := c.sftpArgvExtra(extra)[1:]
	cmd := exec.Command("ssh", args...)
	// A GUI app would otherwise open a console window for ssh, whose
	// closing ends the connection the tree lives on.
	configureEditorCommand(cmd)
	cmd.Env = environmentWithoutSession()
	cleanup := func() {}
	if password != "" {
		env, remove, err := nativeAskpass(gitCredentials{Username: c.User, Password: password})
		if err != nil {
			return nil, nil, err
		}
		cleanup = remove
		cmd.Env = append(cmd.Env, env...)
		// Older ssh builds want a display to use askpass at all.
		cmd.Env = append(cmd.Env, "DISPLAY=:0")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, nil, err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = cmd.Process.Kill()
	})
	client, err := sftp.NewClientPipe(stdout, stdin)
	stop()
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		cleanup()
		message := sshMessage(stderr.String(), err)
		if strings.Contains(strings.ToLower(stderr.String()), "permission denied") {
			return nil, nil, fmt.Errorf("%w: %s", errSSHAuth, message)
		}
		return nil, nil, errors.New(message)
	}
	close := sync.OnceFunc(func() {
		// Kill first so a pending SFTP request cannot hold cleanup open.
		cmd.Process.Kill()
		client.Close()
		cmd.Wait()
		cleanup()
	})
	return sftpRemote{client}, close, nil
}

// sshMessage is the line ssh failed with, or what the tree failed with.
func sshMessage(stderr string, err error) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "Warning:") {
			continue
		}
		return strings.TrimPrefix(line, "ssh: ")
	}
	return err.Error()
}

// sftpRow is an entry of a remote folder.
type sftpRow struct {
	name string
	dir  bool
}

// sftpRows sorts what a directory lists, folders first as the file tree
// does.
func sftpRows(infos []os.FileInfo) []sftpRow {
	rows := make([]sftpRow, 0, len(infos))
	for _, info := range infos {
		if info == nil {
			continue
		}
		name := info.Name()
		if name == "." || name == ".." || name == "" {
			continue
		}
		rows = append(rows, sftpRow{name: name, dir: info.IsDir()})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].dir != rows[j].dir {
			return rows[i].dir
		}
		return strings.ToLower(rows[i].name) < strings.ToLower(rows[j].name)
	})
	return rows
}

// remoteJoin appends a name to a remote directory, whose separator is the
// slash of the host, wherever ShellGrip runs.
func remoteJoin(dir, name string) string {
	if dir == "" {
		dir = "/"
	}
	return path.Join(dir, name)
}
