package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSSHConnectionArgs(t *testing.T) {
	base := append([]string{"ssh"}, sshOptions()...)
	cases := []struct {
		name string
		conn sshConnection
		args []string
	}{
		{
			name: "alias",
			conn: sshConnection{Alias: "omni"},
			args: []string{"omni"},
		},
		{
			name: "fields",
			conn: sshConnection{User: "u", Host: "h", Key: "/k", Jump: "j", Options: []string{"Compression=yes"}},
			args: []string{"-l", "u", "-i", "/k", "-J", "j", "-o", "Compression=yes", "u@h"},
		},
		{
			name: "port",
			conn: sshConnection{User: "u", Host: "h", Port: 2222},
			args: []string{"-l", "u", "-p", "2222", "u@h"},
		},
		{
			name: "default port stays out",
			conn: sshConnection{User: "u", Host: "h", Port: 22},
			args: []string{"-l", "u", "u@h"},
		},
		{
			name: "no follow",
			conn: sshConnection{Alias: "omni", NoFollow: true},
			args: []string{"omni"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := slices.Clone(base)
			if !tc.conn.NoFollow {
				want = append(want, "-t")
			}
			want = append(want, tc.args...)
			if !tc.conn.NoFollow {
				want = append(want, "sh", "-c", "'"+sshBootstrap()+"'")
			}
			if got := tc.conn.argv(); !slices.Equal(got, want) {
				t.Errorf("argv = %q, want %q", got, want)
			}
			sftpArgs := append(slices.Clone(base), tc.args[:len(tc.args)-1]...)
			sftpArgs = append(sftpArgs, "-s", tc.args[len(tc.args)-1], "sftp")
			if got := tc.conn.sftpArgv(); !slices.Equal(got, sftpArgs) {
				t.Errorf("sftpArgv = %q, want %q", got, sftpArgs)
			}
		})
	}
}

func TestSSHBootstrap(t *testing.T) {
	script := sshBootstrap()
	if strings.Contains(script, "'") {
		t.Fatal("the bootstrap has a single quote, which would end the remote command's quoting")
	}
	for _, want := range []string{
		"mktemp -d", "*bash*", "*zsh*", "*fish*", "base64 -d", "ZDOTDIR", `exec "$s" -l`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("the bootstrap lacks %q", want)
		}
	}
	for name, snippet := range map[string]string{
		"bash": bashRC, "zsh": zshRC, "fish": fishHook,
	} {
		if !strings.Contains(script, sshBase64(snippet)) {
			t.Errorf("the bootstrap lacks the %s hook", name)
		}
	}
	if !strings.Contains(bashRC, "__gorex_osc7") || !strings.Contains(zshRC, "precmd_functions") ||
		!strings.Contains(fishHook, "fish_prompt") {
		t.Error("a hook does not report the folder")
	}
	follow := (sshConnection{Alias: "omni"}).followCommand()
	if len(follow) != 3 || follow[0] != "sh" || follow[1] != "-c" ||
		!strings.HasPrefix(follow[2], "'") || !strings.HasSuffix(follow[2], "'") {
		t.Errorf("follow command %q", follow)
	}
	if (sshConnection{NoFollow: true}).followCommand() != nil {
		t.Error("NoFollow still bootstraps the shell")
	}
}

func TestParseSSHConfig(t *testing.T) {
	dir := t.TempDir()
	included := "Host from-include\n  HostName 10.0.0.9\n"
	if err := os.WriteFile(filepath.Join(dir, "extra.conf"), []byte(included), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `
# the work boxes
Include extra.conf

Host omni
  HostName omnivis.my.id
  User omni
  Port 2222
  IdentityFile ~/.ssh/id_ed25519
  ProxyJump bastion

Host gp-landing impala
  HostName 10.34.167.52
  User 80208379

Host *.example
  User root

Match host ignored
  User nobody

Host quoted
  HostName "example.com"
  IdentityFile "/keys/a b"
`
	hosts := parseSSHConfig([]byte(config), dir, map[string]bool{})
	byAlias := map[string]sshConfigHost{}
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}
	if len(hosts) != 5 {
		t.Fatalf("%d hosts: %+v", len(hosts), hosts)
	}
	omni, ok := byAlias["omni"]
	if !ok || omni.HostName != "omnivis.my.id" || omni.User != "omni" || omni.Port != 2222 || omni.Jump != "bastion" {
		t.Errorf("omni = %+v", omni)
	}
	if !strings.HasSuffix(omni.Key, filepath.Join(".ssh", "id_ed25519")) {
		t.Errorf("omni key = %q", omni.Key)
	}
	for _, alias := range []string{"gp-landing", "impala"} {
		h, ok := byAlias[alias]
		if !ok || h.HostName != "10.34.167.52" || h.User != "80208379" || h.Port != 22 {
			t.Errorf("%s = %+v", alias, h)
		}
	}
	if _, ok := byAlias["*.example"]; ok {
		t.Error("a wildcard became a connection")
	}
	if h, ok := byAlias["from-include"]; !ok || h.HostName != "10.0.0.9" {
		t.Errorf("include = %+v", h)
	}
	if h, ok := byAlias["quoted"]; !ok || h.HostName != "example.com" || h.Key != "/keys/a b" {
		t.Errorf("quoted = %+v", h)
	}
}

func TestImportSSHConnections(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOREX_DIR", t.TempDir())
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "Host omni\n  HostName omnivis.my.id\n  User omni\n\nHost box\n  HostName box.local\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	n, err := a.importSSHConfigDir(dir)
	if err != nil || n != 2 {
		t.Fatalf("imported %d, %v", n, err)
	}
	if len(a.sshConns) != 2 || a.sshConns[0].Alias != "omni" {
		t.Fatalf("connections %+v", a.sshConns)
	}
	if _, err := os.Stat(sshPath()); err != nil {
		t.Errorf("ssh.json not saved: %v", err)
	}
	if again, _ := a.importSSHConfigDir(dir); again != 0 {
		t.Errorf("imported %d twice", again)
	}
}

func TestSSHConnectionsFile(t *testing.T) {
	t.Setenv("GOREX_DIR", t.TempDir())
	conns := []sshConnection{
		{ID: "one", Name: "omni", Alias: "omni", Port: 22},
		{ID: "two", Name: "Box", User: "u", Host: "box.local", Port: 2222},
	}
	if err := saveSSHConnections(conns); err != nil {
		t.Fatal(err)
	}
	got := loadSSHConnections()
	if len(got) != 2 || got[0].Alias != "omni" || got[1].Port != 2222 {
		t.Fatalf("loaded %+v", got)
	}
}

func TestSSHDraft(t *testing.T) {
	if _, err := (&sshDraft{Port: "22"}).connection(); err == nil {
		t.Error("a connection without a host was saved")
	}
	if _, err := (&sshDraft{Host: "h", Port: "x"}).connection(); err == nil {
		t.Error("a bad port was saved")
	}
	if _, err := (&sshDraft{Alias: "-oProxyCommand=evil"}).connection(); err == nil {
		t.Error("an option as an alias was saved")
	}
	c, err := (&sshDraft{Alias: "omni", Port: "22", Options: "Compression=yes, "}).connection()
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Name != "omni" || !slices.Equal(c.Options, []string{"Compression=yes"}) {
		t.Errorf("alias connection %+v", c)
	}
	c, err = (&sshDraft{Name: "Box", User: "u", Host: "h", Port: "2222"}).connection()
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Box" || c.Port != 2222 || c.dest() != "u@h" {
		t.Errorf("field connection %+v", c)
	}
	c, err = (&sshDraft{Host: "h", NoFollow: true}).connection()
	if err != nil || !c.NoFollow {
		t.Errorf("no-follow connection %+v, %v", c, err)
	}
	if again := newSSHDraft(c); !again.NoFollow || again.Host != "h" {
		t.Errorf("draft %+v", again)
	}
}

func TestSSHProfile(t *testing.T) {
	home, _ := os.UserHomeDir()
	c := sshConnection{ID: "one", Alias: "omni", StartDir: "/srv/www"}
	p := c.profile()
	if p.SSH == nil || p.SSH.Alias != "omni" || p.ID != "ssh:one" {
		t.Fatalf("profile %+v", p)
	}
	cmd, dir := p.launch("/remote/place")
	if !slices.Equal(cmd, c.argv()) || dir != home {
		t.Errorf("launch = %q in %q", cmd, dir)
	}
	if got := p.startingDir("/remote/place"); got != home {
		t.Errorf("starting dir = %q, want %q", got, home)
	}
	pane := &Pane{profile: p}
	if name, detail := pane.label(); name != "omni" || detail != "SSH" {
		t.Errorf("label = %q %q", name, detail)
	}
	if prog := paneProgram(pane); prog.Name != "SSH" {
		t.Errorf("program = %q", prog.Name)
	}
	box := sshConnection{ID: "two", Name: "Box", User: "u", Host: "box.local"}.profile()
	pane = &Pane{profile: box}
	if name, detail := pane.label(); name != "Box" || detail != "u@box.local" {
		t.Errorf("box label = %q %q", name, detail)
	}
}
