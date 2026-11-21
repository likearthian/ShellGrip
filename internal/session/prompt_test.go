package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestApplyCustomPrompt(t *testing.T) {
	t.Run("pwsh", func(t *testing.T) {
		argv, env := applyCustomPrompt("pwsh.exe", []string{"pwsh.exe"}, "pwsh.exe", []string{"FOO=1"})
		if len(argv) != 4 || argv[1] != "-NoExit" || argv[2] != "-Command" || argv[3] != pwshPromptScript {
			t.Fatalf("unexpected pwsh argv: %v", argv)
		}
		if !slices.Equal(env, []string{"FOO=1"}) {
			t.Fatalf("unexpected env: %v", env)
		}
	})

	t.Run("pwsh replaces built-in shellgrip prompt wrapper", func(t *testing.T) {
		in := []string{"pwsh.exe", "-NoExit", "-Command", "$global:__ShellGripPrompt = $function:prompt"}
		argv, _ := applyCustomPrompt("pwsh.exe", in, "pwsh.exe", nil)
		if len(argv) != 4 || argv[3] != pwshPromptScript {
			t.Fatalf("expected built-in wrapper replaced, got %v", argv)
		}
	})

	t.Run("pwsh non-interactive unchanged", func(t *testing.T) {
		in := []string{"pwsh.exe", "-Command", "echo hi"}
		argv, _ := applyCustomPrompt("pwsh.exe", in, "pwsh.exe", nil)
		if !slices.Equal(argv, in) {
			t.Fatalf("expected unchanged argv, got %v", argv)
		}
	})

	t.Run("bash strips login flag and adds rcfile", func(t *testing.T) {
		argv, _ := applyCustomPrompt("/bin/bash", []string{"-bash", "--login"}, "bash", nil)
		if argv[0] != "bash" {
			t.Fatalf("expected leading dash stripped, got %q", argv[0])
		}
		if slices.Contains(argv, "--login") {
			t.Fatalf("expected --login stripped, got %v", argv)
		}
		idx := slices.Index(argv, "--rcfile")
		if idx < 0 || idx+1 >= len(argv) || !strings.HasSuffix(argv[idx+1], "shellgrip-prompt.bash") {
			t.Fatalf("expected --rcfile shellgrip-prompt.bash, got %v", argv)
		}
		if !slices.Contains(argv, "-i") {
			t.Fatalf("expected -i in bash argv, got %v", argv)
		}
	})

	t.Run("zsh preserves original ZDOTDIR", func(t *testing.T) {
		in := []string{"-zsh"}
		argv, env := applyCustomPrompt("/bin/zsh", in, "zsh", []string{"ZDOTDIR=/custom/zsh", "A=1"})
		if !slices.Equal(argv, in) {
			t.Fatalf("expected zsh argv unchanged, got %v", argv)
		}
		if !slices.Contains(env, "SHELLGRIP_ORIG_ZDOTDIR=/custom/zsh") {
			t.Fatalf("expected SHELLGRIP_ORIG_ZDOTDIR preserved, got %v", env)
		}
		var zdotdir string
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "ZDOTDIR="); ok {
				zdotdir = v
			}
		}
		if zdotdir == "" {
			t.Fatalf("expected ZDOTDIR set, got %v", env)
		}
		data, err := os.ReadFile(filepath.Join(zdotdir, ".zshenv"))
		if err != nil {
			t.Fatalf("reading .zshenv: %v", err)
		}
		if !strings.Contains(string(data), "133;A") || !strings.Contains(string(data), "133;B") {
			t.Fatalf("expected OSC 133 markers in .zshenv")
		}
	})

	t.Run("fish", func(t *testing.T) {
		argv, _ := applyCustomPrompt("/usr/bin/fish", []string{"-fish"}, "fish", nil)
		if len(argv) != 3 || argv[1] != "-C" || !strings.Contains(argv[2], "shellgrip-prompt.fish") {
			t.Fatalf("unexpected fish argv: %v", argv)
		}
	})

	t.Run("wsl", func(t *testing.T) {
		in := []string{"wsl.exe", "--distribution", "Ubuntu", "--cd", "~"}
		argv, _ := applyCustomPrompt("wsl.exe", in, "wsl.exe", nil)
		if len(argv) != len(in)+4 || argv[len(in)] != "--exec" || argv[len(in)+1] != "sh" || argv[len(in)+2] != "-c" {
			t.Fatalf("unexpected wsl argv: %v", argv)
		}
		if strings.Contains(argv[len(in)+3], "\n") {
			t.Fatalf("wsl bootstrap command must be single-line")
		}
		if !strings.Contains(argv[len(in)+3], "shellgrip-prompt.bash") || !strings.Contains(argv[len(in)+3], "ZDOTDIR") {
			t.Fatalf("expected shell bootstrap in wsl command")
		}
	})

	t.Run("wsl with explicit command unchanged", func(t *testing.T) {
		in := []string{"wsl.exe", "--distribution", "Ubuntu", "--", "ls"}
		argv, _ := applyCustomPrompt("wsl.exe", in, "wsl.exe", nil)
		if !slices.Equal(argv, in) {
			t.Fatalf("expected wsl with -- unchanged, got %v", argv)
		}
	})

	t.Run("unsupported shells unchanged", func(t *testing.T) {
		for _, shell := range []string{"cmd.exe", "ssh"} {
			in := []string{shell}
			argv, _ := applyCustomPrompt(shell, in, shell, nil)
			if !slices.Equal(argv, in) {
				t.Fatalf("expected %s unchanged, got %v", shell, argv)
			}
		}
	})
}
