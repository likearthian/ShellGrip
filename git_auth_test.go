package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo"
)

// TestMain lets the tests run as the askpass helper too, as the app does.
func TestMain(m *testing.M) {
	if secret := os.Getenv("SHELLGRIP_ASKPASS_SECRET"); secret != "" {
		runAskpass(secret, os.Args[1:])
		return
	}
	if os.Getenv("SHELLGRIP_PREVIEW_NATIVE_TEST") == "1" {
		// native tests need the event loop on the process' main thread.
		result := make(chan int, 1)
		go func() {
			result <- m.Run()
			mygo.App.Quit()
		}()
		if err := mygo.App.Run(); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
			os.Exit(1)
		}
		os.Exit(<-result)
	}
	os.Exit(m.Run())
}

func TestGitNeedsCredentials(t *testing.T) {
	for _, out := range []string{
		"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
		"fatal: could not read Password for 'https://user@github.com': terminal prompts disabled",
		"fatal: Authentication failed for 'https://github.com/x/y.git/'",
		"git@github.com: Permission denied (publickey).",
		"Enter passphrase for key '/home/user/.ssh/id_ed25519':",
		"remote: Invalid username or password.",
		"remote: HTTP Basic: Access denied",
	} {
		if !gitNeedsCredentials([]byte(out)) {
			t.Errorf("%q is not seen as needing credentials", out)
		}
	}
	for _, out := range []string{
		"fatal: not a git repository (or any of the parent directories): .git",
		"fatal: The current branch main has no upstream branch.",
		"fatal: Host key verification failed.",
		"error: failed to push some refs",
	} {
		if gitNeedsCredentials([]byte(out)) {
			t.Errorf("%q is seen as needing credentials", out)
		}
	}
}

func TestGitAuthUsername(t *testing.T) {
	out := []byte("fatal: could not read Password for 'https://ziska@github.com': terminal prompts disabled")
	if got := gitAuthUsername(out); got != "ziska" {
		t.Fatalf("username %q", got)
	}
	if got := gitAuthUsername([]byte("fatal: could not read Username for 'https://github.com':")); got != "" {
		t.Fatalf("username %q, want empty", got)
	}
}

func TestAskpassAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(path, []byte(`{"username":"ziska","password":"s3cret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ prompt, want string }{
		{"Username for 'https://github.com':", "ziska"},
		{"Login for 'https://github.com':", "ziska"},
		{"Password for 'https://ziska@github.com':", "s3cret"},
		{"Enter passphrase for key '/home/ziska/.ssh/id_ed25519':", "s3cret"},
	} {
		got, err := askpassAnswer(path, []string{tc.prompt})
		if err != nil {
			t.Fatal(err)
		}
		if got != tc.want {
			t.Errorf("%q: %q, want %q", tc.prompt, got, tc.want)
		}
	}
}

func TestWithEnvironment(t *testing.T) {
	env := withEnvironment([]string{"PATH=/bin", "GIT_ASKPASS=old"}, "GIT_ASKPASS=new", "SSH_ASKPASS=ssh")
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GIT_ASKPASS=old") || !strings.Contains(joined, "GIT_ASKPASS=new") || !strings.Contains(joined, "SSH_ASKPASS=ssh") {
		t.Fatalf("environment %q", env)
	}
}

func TestNativeAskpass(t *testing.T) {
	env, cleanup, err := nativeAskpass(gitCredentials{Username: "ziska", Password: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	exe := ""
	for _, value := range env {
		if strings.HasPrefix(value, "GIT_ASKPASS=") {
			exe = strings.TrimPrefix(value, "GIT_ASKPASS=")
		}
	}
	if exe == "" {
		t.Fatal("no askpass program")
	}
	for _, tc := range []struct{ prompt, want string }{
		{"Username for 'https://github.com':", "ziska"},
		{"Password for 'https://ziska@github.com':", "s3cret"},
		{"Enter passphrase for key '/home/ziska/.ssh/id_ed25519':", "s3cret"},
	} {
		cmd := exec.Command(exe, tc.prompt)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", tc.prompt, err, out)
		}
		if got := strings.TrimSpace(string(out)); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.prompt, got, tc.want)
		}
	}
}

// TestNativeGitAuthentication makes git ask for a password against a
// server that wants one, and checks that the askpass helper answers.
func TestNativeGitAuthentication(t *testing.T) {
	requireGit(t)
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "credential.helper")
	t.Setenv("GIT_CONFIG_VALUE_0", "")

	var mu sync.Mutex
	got := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="shellgrip"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mu.Lock()
		got = user + ":" + password
		mu.Unlock()
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _ = gitRunWithAuth(&Pane{}, t.TempDir(), &gitCredentials{Username: "ziska", Password: "s3cret"}, "ls-remote", server.URL+"/repo.git")
	mu.Lock()
	defer mu.Unlock()
	if got != "ziska:s3cret" {
		t.Fatalf("git sent %q, want ziska:s3cret", got)
	}
}

func TestGitAuthDialog(t *testing.T) {
	a, tt := newTestApp(t)
	p := a.tab().Focus
	a.gitAuth = gitAuthState{
		open: true, pane: p, dir: p.directory(),
		commands: [][]string{{"status"}},
		title:    "Authentication required",
		detail:   "Git needs credentials.",
		username: "ziska",
	}
	tt.Frame()
	for _, text := range []string{"Authentication required", "Username", "Password", "Continue", "Cancel"} {
		if _, ok := tt.Find(text); !ok {
			t.Fatalf("the credential dialog lacks %q; texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("Password"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("s3cret")
	tt.Frame()
	if a.gitAuth.password != "s3cret" {
		t.Fatalf("password %q", a.gitAuth.password)
	}
	if _, ok := tt.Find("s3cret"); ok {
		t.Fatal("the credential dialog exposes the password")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.gitAuth.open {
		t.Fatal("Cancel keeps the credential dialog open")
	}
	if a.gitAuth.password != "" {
		t.Fatal("Cancel keeps the password")
	}
}
