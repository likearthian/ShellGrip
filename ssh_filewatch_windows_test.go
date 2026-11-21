//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
)

// Uses a temporary loopback sshd, never a saved/production SSH connection.
func TestSSHRelayLoopback(t *testing.T) {
	distro := os.Getenv("GOREX_TEST_SSH_WATCH")
	if distro == "" {
		t.Skip("set GOREX_TEST_SSH_WATCH to a WSL distribution with OpenSSH server")
	}
	run := func(script string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--exec", "sh", "-s")
		cmd.Stdin = strings.NewReader(script)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	root, err := run("mktemp -d /tmp/gorex-ssh-watch-XXXXXXXX")
	if err != nil {
		t.Skipf("WSL unavailable: %v %s", err, root)
	}
	if !strings.HasPrefix(root, "/tmp/gorex-ssh-watch-") || strings.ContainsAny(root, "'\r\n") {
		t.Fatal("invalid fixture directory")
	}
	t.Cleanup(func() { run("rm -rf -- " + sshQuote(root)) })
	user, err := run("id -un")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKey(priv, "GoRex test")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	// OpenSSH rejects the sandbox SID inherited by Windows temporary files.
	userToken, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + userToken.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(key, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	public, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	p := &Pane{profile: terminalProfile{ID: "wsl:" + distro, WSL: true}}
	if err := os.WriteFile(fileTreeDirectory(p, root+"/authorized_keys"), gossh.MarshalAuthorizedKey(public), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := run("ssh-keygen -q -t ed25519 -N '' -f " + sshQuote(root+"/host")); err != nil {
		t.Skipf("host key: %v %s", err, out)
	}
	config := fmt.Sprintf("Port %d\nListenAddress 127.0.0.1\nHostKey %s/host\nPidFile %s/pid\nAuthorizedKeysFile %s/authorized_keys\nStrictModes no\nUsePAM yes\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\nAllowUsers %s\nSubsystem sftp internal-sftp\nLogLevel DEBUG\n", port, root, root, root, user)
	if err := os.WriteFile(fileTreeDirectory(p, root+"/config"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	serverCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	server := exec.CommandContext(serverCtx, "wsl.exe", "-d", distro, "--exec", "/usr/sbin/sshd", "-D", "-e", "-f", root+"/config")
	var serverErr bytes.Buffer
	server.Stderr = &serverErr
	if err := server.Start(); err != nil {
		t.Skipf("sshd: %v", err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Wait() }()
	t.Cleanup(func() {
		run("if [ -f " + sshQuote(root+"/pid") + " ]; then kill $(cat " + sshQuote(root+"/pid") + "); fi")
		stopServer()
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
		}
	})
	for deadline := time.Now().Add(5 * time.Second); ; {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		select {
		case err := <-serverDone:
			t.Fatalf("temporary sshd failed: %v: %s", err, serverErr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("loopback SSH did not become reachable")
		}
		time.Sleep(50 * time.Millisecond)
	}
	c := sshConnection{Host: "127.0.0.1", User: user, Port: port, Key: key, Options: []string{"BatchMode=yes", "HostName=127.0.0.1", "ProxyCommand=none", "ProxyJump=none", "IdentityAgent=none", "IdentitiesOnly=yes", "PermitLocalCommand=no", "StrictHostKeyChecking=no", "UserKnownHostsFile=" + filepath.ToSlash(filepath.Join(dir, "known_hosts"))}}
	fs, closeFS, err := dialSFTP(c, "")
	if err != nil {
		probeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		debugConn := c
		debugConn.Options = append(append([]string{}, c.Options...), "LogLevel=DEBUG2")
		probe, clean, _ := sshRelayCommand(probeCtx, debugConn, "", "true")
		debugOut, _ := probe.CombinedOutput()
		clean()
		stop()
		run("if [ -f " + sshQuote(root+"/pid") + " ]; then kill $(cat " + sshQuote(root+"/pid") + "); fi")
		stopServer()
		select {
		case <-serverDone:
		case <-time.After(3 * time.Second):
		}
		var useful []string
		for _, line := range strings.Split(string(debugOut), "\n") {
			lower := strings.ToLower(line)
			for _, word := range []string{"load key", "sign", "permission", "private", "warning", "identity", "failed"} {
				if strings.Contains(lower, word) {
					useful = append(useful, line)
					break
				}
			}
		}
		t.Fatalf("%v\n%s", err, strings.Join(useful, "\n"))
	}
	defer closeFS()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	platform, err := sshRelayOutput(ctx, c, "", "uname -m")
	if err != nil {
		t.Fatal(err)
	}
	data, err := relayBinary(platform)
	if err != nil {
		t.Fatal(err)
	}
	cached := func(name, sum string) bool {
		out, err := sshRelayOutput(ctx, c, "", "sha256sum -- "+sshQuote(name))
		return err == nil && strings.HasPrefix(out, sum+" ")
	}
	var transferred int64
	helper, err := deploySSHRelay(ctx, fs.(relayFS), root, data, cached, func(sent, total int64) { transferred = sent })
	if err != nil {
		t.Fatal(err)
	}
	if transferred != int64(len(data)) {
		t.Fatal("upload did not complete")
	}
	_, err = deploySSHRelay(ctx, fs.(relayFS), root, data, cached, func(int64, int64) { t.Fatal("cached copy was reuploaded") })
	if err != nil {
		t.Fatal(err)
	}
	out, err := sshRelayOutput(ctx, c, "", "sha256sum -- "+sshQuote(helper))
	if err != nil || !strings.HasPrefix(out, fmt.Sprintf("%x", sha256.Sum256(data))) {
		t.Fatal("deployed executable checksum differs")
	}
	ready := make(chan struct{}, 1)
	events := make(chan struct{}, 4)
	done := make(chan error, 1)
	go func() {
		done <- runSSHRelay(ctx, c, "", helper, root, func() { ready <- struct{}{} }, func() { events <- struct{}{} })
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal("relay did not become ready")
	}
	processes, err := run("ps -eo pid,args")
	if err != nil {
		t.Fatal(err)
	}
	pid := ""
	for _, line := range strings.Split(processes, "\n") {
		if strings.Contains(line, helper+" "+root+" tree") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				pid = fields[0]
				break
			}
		}
	}
	if _, err := strconv.Atoi(pid); err != nil {
		t.Fatal("remote helper PID not found")
	}
	if _, err := run("printf changed > " + sshQuote(root+"/notes.txt")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-ctx.Done():
		t.Fatal("SSH watcher missed remote save")
	}
	oldPrefs := prefs
	prefs.FileTree = false
	t.Cleanup(func() { prefs = oldPrefs })
	a, tt := newTestApp(t)
	pane := a.tab().Focus
	pane.profile = c.profile()
	session := &sftpSession{app: a, pane: pane, fs: fs, home: root}
	if out, err := run("printf first > " + sshQuote(root+"/preview.txt") + "; touch -m -d @1 " + sshQuote(root+"/preview.txt")); err != nil {
		t.Fatalf("preview fixture: %v %s", err, out)
	}
	a.openRemotePreview(pane, &remoteTreeState{session: session}, root+"/preview.txt")
	waitFor(t, tt, "remote preview", func() bool { return a.tab().preview != nil && !a.tab().preview.loading })
	preview := a.tab().preview
	if out, err := run("printf other > " + sshQuote(root+"/preview.txt") + "; touch -m -d @1 " + sshQuote(root+"/preview.txt")); err != nil {
		t.Fatalf("preview save: %v %s", err, out)
	}
	a.refreshSSHPreview(session, true)
	waitFor(t, tt, "same-size remote save", func() bool { return string(preview.content.source) == "other" })
	cancel()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("SSH watcher did not stop")
	}
	if out, err := run("test ! -d /proc/" + pid); err != nil {
		t.Fatalf("remote helper survived cancellation: %v %s", err, out)
	}
	t.Log("SFTP upload, SHA-256 verification, cached reuse, live SSH notifications, equal-size preview refresh, and cancellation passed on port " + strconv.Itoa(port))
}
