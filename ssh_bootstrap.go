package main

import (
	"encoding/base64"
	"strings"
)

// The hooks that report a shell's folder over OSC 7. Each sources the
// user's own startup files first, so the shell starts as it would have.
const (
	bashRC = `if [ -f "$HOME/.bash_profile" ]; then . "$HOME/.bash_profile"
elif [ -f "$HOME/.bash_login" ]; then . "$HOME/.bash_login"
elif [ -f "$HOME/.profile" ]; then . "$HOME/.profile"
fi
__shellgrip_osc7() { printf '\033]7;file://%s%s\a' "${HOSTNAME:-}" "$PWD"; }
PROMPT_COMMAND="__shellgrip_osc7${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
`

	zshEnv = `[ -f "$HOME/.zshenv" ] && . "$HOME/.zshenv"
`

	zshProfile = `[ -f "$HOME/.zprofile" ] && . "$HOME/.zprofile"
`

	zshRC = `[ -f "$HOME/.zshrc" ] && . "$HOME/.zshrc"
__shellgrip_osc7() { printf '\033]7;file://%s%s\a' "${HOST:-}" "$PWD" }
precmd_functions+=(__shellgrip_osc7)
`

	zshLogin = `[ -f "$HOME/.zlogin" ] && . "$HOME/.zlogin"
`

	fishHook = `function __shellgrip_osc7 --on-event fish_prompt
    set -l h (hostname)
    printf '\x1b]7;file://%s%s\x07' $h $PWD
end
`
)

func sshBase64(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// sshBootstrap returns a POSIX sh script that starts the user's shell with
// a hook reporting its folder, so the file tree can follow cd. The caller
// quotes it in single quotes, which it has none of: the shell snippets are
// base64, and the script itself uses only double quotes.
func sshBootstrap() string {
	var b strings.Builder
	b.WriteString(`d=$(mktemp -d "${TMPDIR:-/tmp}/shellgrip.XXXXXX") || exec "${SHELL:-/bin/sh}" -l
s=${SHELL:-/bin/sh}
write() { printf %s "$2" | base64 -d > "$1" 2>/dev/null || printf %s "$2" | base64 -D > "$1" 2>/dev/null || printf %s "$2" | openssl base64 -d -A > "$1" 2>/dev/null; }
case "$s" in
*bash*)
write "$d/rc" "`)
	b.WriteString(sshBase64(bashRC))
	b.WriteString(`" || exec "$s" -l
exec "$s" --init-file "$d/rc" -i
;;
*zsh*)
write "$d/.zshenv" "`)
	b.WriteString(sshBase64(zshEnv))
	b.WriteString(`" || exec "$s" -l
write "$d/.zprofile" "`)
	b.WriteString(sshBase64(zshProfile))
	b.WriteString(`" || exec "$s" -l
write "$d/.zshrc" "`)
	b.WriteString(sshBase64(zshRC))
	b.WriteString(`" || exec "$s" -l
write "$d/.zlogin" "`)
	b.WriteString(sshBase64(zshLogin))
	b.WriteString(`" || exec "$s" -l
ZDOTDIR="$d"
export ZDOTDIR
exec "$s" -l -i
;;
*fish*)
write "$d/hook.fish" "`)
	b.WriteString(sshBase64(fishHook))
	b.WriteString(`" || exec "$s" -l
exec "$s" -l -C "source $d/hook.fish"
;;
*)
exec "$s" -l
;;
esac
`)
	return b.String()
}

// followCommand is the remote command that starts the shell with the hook.
func (c sshConnection) followCommand() []string {
	if c.NoFollow {
		return nil
	}
	return []string{"sh", "-c", "'" + sshBootstrap() + "'"}
}
