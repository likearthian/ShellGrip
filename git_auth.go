package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// gitCredentials is what the user enters when git asks for a password.
type gitCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// gitAuthState is the credential dialog of a git action that needs them.
type gitAuthState struct {
	open     bool
	pane     *Pane
	dir      string
	commands [][]string
	title    string
	detail   string
	username string
	password string
	remember bool
	error    string
}

// gitNeedsCredentials reports whether git stopped because it could not
// ask for a username, a password, or a passphrase.
func gitNeedsCredentials(out []byte) bool {
	text := strings.ToLower(string(out))
	for _, marker := range []string{
		"could not read username",
		"could not read password",
		"terminal prompts disabled",
		"authentication failed",
		"permission denied (publickey",
		"enter passphrase for key",
		"invalid username or password",
		"invalid credentials",
		"http basic: access denied",
		"returned error: 401",
		"support for password authentication was removed",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// gitAuthUsername is the user a failed URL already names, as
// "https://user@host", for the dialog's username field.
func gitAuthUsername(out []byte) string {
	match := gitURLUserPattern.FindSubmatch(out)
	if len(match) < 2 {
		return ""
	}
	return string(match[1])
}

var gitURLUserPattern = regexp.MustCompile(`://([^/@:\s]+)@`)

// firstLine is the part of git's error that fits the dialog's detail.
func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		message = message[:i]
	}
	message = strings.TrimSpace(message)
	for _, prefix := range []string{"fatal: ", "error: ", "remote: "} {
		message = strings.TrimPrefix(message, prefix)
	}
	if len(message) > 300 {
		message = message[:300] + "…"
	}
	return message
}

// runAskpass is the helper git runs for a prompt: it prints the username
// or password the app collected. ShellGrip starts in this mode when git runs
// it through GIT_ASKPASS or SSH_ASKPASS.
func runAskpass(path string, args []string) {
	answer, err := askpassAnswer(path, args)
	if err != nil {
		os.Exit(1)
	}
	fmt.Println(answer)
}

// askpassAnswer reads the secret and picks the half the prompt asks for.
func askpassAnswer(path string, args []string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var creds gitCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}
	prompt := strings.ToLower(strings.Join(args, " "))
	if strings.Contains(prompt, "username") || strings.Contains(prompt, "login for") {
		return creds.Username, nil
	}
	return creds.Password, nil
}

// nativeAskpass writes the secret for the app's askpass mode and returns
// the environment that points git at it.
func nativeAskpass(creds gitCredentials) ([]string, func(), error) {
	path, err := writeAskpassSecret(creds)
	if err != nil {
		return nil, nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		os.Remove(path)
		return nil, nil, err
	}
	env := []string{
		"GIT_ASKPASS=" + exe,
		"SSH_ASKPASS=" + exe,
		"SSH_ASKPASS_REQUIRE=force",
		"SHELLGRIP_ASKPASS_SECRET=" + path,
	}
	return env, func() { os.Remove(path) }, nil
}

// writeAskpassSecret keeps the secret in a file only the user can read.
func writeAskpassSecret(creds gitCredentials) (string, error) {
	data, err := json.Marshal(creds)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "shellgrip-askpass-*.json")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// wslAskpass writes an askpass script and the secret inside the pane's
// distribution, where its git runs, and returns the environment that
// points git at them.
func wslAskpass(p *Pane, creds gitCredentials) ([]string, func(), error) {
	if p == nil || len(p.profile.Command) == 0 {
		return nil, nil, fmt.Errorf("no WSL distribution command")
	}
	base := fmt.Sprintf("/tmp/shellgrip-askpass-%d-%d", os.Getpid(), time.Now().UnixNano())
	script, user, password := base+".sh", base+".user", base+".pass"
	body := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in\n  *sername*|*ogin\\ for*) cat '%s' ;;\n  *) cat '%s' ;;\nesac\n", user, password)
	if err := wslWriteFile(p, script, body, "700"); err != nil {
		return nil, nil, err
	}
	if err := wslWriteFile(p, user, creds.Username, "600"); err != nil {
		wslRemoveFiles(p, script)
		return nil, nil, err
	}
	if err := wslWriteFile(p, password, creds.Password, "600"); err != nil {
		wslRemoveFiles(p, script, user)
		return nil, nil, err
	}
	env := []string{
		"GIT_ASKPASS=" + script,
		"SSH_ASKPASS=" + script,
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=:0",
	}
	return env, func() { wslRemoveFiles(p, script, user, password) }, nil
}

// wslWriteFile writes a file inside the pane's distribution, with mode.
func wslWriteFile(p *Pane, path, content, mode string) error {
	args := append([]string(nil), p.profile.Command[1:]...)
	args = append(args, "--exec", "sh", "-c", `umask 077; cat > "$1"; chmod `+mode+` "$1"`, "shellgrip", path)
	cmd := exec.Command(p.profile.Command[0], args...)
	configureEditorCommand(cmd)
	cmd.Env = environmentWithoutSession()
	cmd.Stdin = strings.NewReader(content)
	return cmd.Run()
}

// wslRemoveFiles removes the askpass files from the distribution.
func wslRemoveFiles(p *Pane, paths ...string) {
	if p == nil || len(p.profile.Command) == 0 || len(paths) == 0 {
		return
	}
	args := append([]string(nil), p.profile.Command[1:]...)
	args = append(args, "--exec", "rm", "-f")
	args = append(args, paths...)
	cmd := exec.Command(p.profile.Command[0], args...)
	configureEditorCommand(cmd)
	cmd.Env = environmentWithoutSession()
	cmd.Run()
}
