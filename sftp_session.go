package main

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// sftpSession is a pane's SFTP connection, which the sidebar's remote
// tree reads through.
type sftpSession struct {
	watchCtx    context.Context
	watchCancel context.CancelFunc
	app         *App
	pane        *Pane

	fs      remoteFS
	closeFS func()
	home    string

	connecting       bool
	err              string
	needPassword     bool
	password         string
	rememberPassword bool
	closed           bool
	treeReading      bool
}

// sftpFor returns the pane's session, making one when it has none yet.
func (a *App) sftpFor(p *Pane) *sftpSession {
	if a.sftpSessions == nil {
		a.sftpSessions = map[int]*sftpSession{}
	}
	if s := a.sftpSessions[p.ID]; s != nil {
		return s
	}
	s := &sftpSession{app: a, pane: p}
	a.sftpSessions[p.ID] = s
	return s
}

// closeSFTPForPane ends a pane's SFTP connection.
func (a *App) closeSFTPForPane(p *Pane) {
	s := a.sftpSessions[p.ID]
	if s == nil {
		return
	}
	delete(a.sftpSessions, p.ID)
	s.close()
}

// closeSFTPSessions ends every connection, as the app quits or its window
// goes away.
func (a *App) closeSFTPSessions() {
	for id, s := range a.sftpSessions {
		delete(a.sftpSessions, id)
		s.close()
	}
}

// ensure connects a session that has not tried yet. A failure waits for
// the user's Reconnect, so a dead host is not asked again every frame.
func (s *sftpSession) ensure() {
	if s == nil || s.err != "" || s.needPassword {
		return
	}
	s.connect()
}

// connect asks the host for the sftp subsystem, off the frame that asked,
// so a slow network never holds the sidebar.
func (s *sftpSession) connect() {
	if s == nil || s.connecting || s.fs != nil || s.closed {
		return
	}
	conn := s.pane.profile.SSH
	if conn == nil {
		return
	}
	if s.watchCtx == nil || s.watchCtx.Err() != nil {
		s.watchCtx, s.watchCancel = context.WithCancel(context.Background())
	}
	s.connecting = true
	s.err = ""
	s.needPassword = false
	password := s.password
	remember := s.rememberPassword
	parent := s.watchCtx
	go func() {
		if password == "" {
			password = loadSSHPassword(*conn)
		}
		dialCtx, cancel := context.WithTimeout(parent, 30*time.Second)
		fs, closeFS, err := dialSFTPContext(dialCtx, *conn, password)
		home := ""
		if err == nil {
			home, err = sftpHome(dialCtx, fs, closeFS)
			if err == nil && remember && password != "" {
				saveSSHPassword(*conn, password)
			}
		} else if errors.Is(err, errSSHAuth) {
			deleteSSHPassword(*conn)
		}
		cancel()
		if parent.Err() != nil {
			if err == nil && closeFS != nil {
				closeFS()
			}
			return
		}
		mygo.RunOnMain(func() {
			if s.closed {
				s.connecting = false
				if err == nil && closeFS != nil {
					go closeFS()
				}
				return
			}
			s.connecting = false
			if err != nil {
				s.err = err.Error()
				s.needPassword = errors.Is(err, errSSHAuth)
				s.notify()
				return
			}
			s.fs, s.closeFS = fs, closeFS
			s.err, s.needPassword = "", false
			if home == "" {
				home = "/"
			}
			s.home = home
			s.notify()
		})
	}()
}

// sftpHome keeps setup cancellation attached until the first request finishes.
func sftpHome(ctx context.Context, fs remoteFS, closeFS func()) (string, error) {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		closeFS()
		close(done)
	})
	home, _ := fs.Getwd()
	if !stop() {
		<-done
		return "", ctx.Err()
	}
	return home, nil
}

func (s *sftpSession) close() {
	s.closed = true
	if s.watchCancel != nil {
		s.watchCancel()
	}
	if s.app != nil {
		s.app.closeSSHWatchFor(s)
	}
	if s.closeFS != nil {
		s.closeFS()
		s.closeFS = nil
	}
	s.fs = nil
}

// drop marks the connection lost, keeping the reason for the sidebar. The
// ssh process is closed off the frame, as its Wait may take a moment.
func (s *sftpSession) drop(message string) {
	if s.watchCancel != nil {
		s.watchCancel()
	}
	if s.app != nil {
		s.app.closeSSHWatchFor(s)
	}
	if s.closeFS != nil {
		close := s.closeFS
		s.closeFS = nil
		go close()
	}
	s.fs = nil
	s.err = message
	s.needPassword = false
}

// sftpConnLost reports whether err means the connection itself ended, as
// when its ssh process goes, rather than a folder that cannot be read.
func sftpConnLost(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, os.ErrClosed) || errors.Is(err, net.ErrClosed) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"connection lost", "broken pipe", "connection reset",
		"use of closed network connection", "unexpected eof", "failed to send packet",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// closeSFTPPane ends the SFTP connection of a pane that ends, and the
// remote tree that followed it.
func (a *App) closeSFTPPane(p *Pane) {
	a.closeSFTPForPane(p)
	if a.remoteTree != nil && a.remoteTree.pane == p {
		a.remoteTree = nil
	}
}

// notify draws again the sidebar that shows the connection.
func (s *sftpSession) notify() {
	a := s.app
	if a == nil {
		return
	}
	if a.win != nil {
		a.win.Invalidate()
	}
}
