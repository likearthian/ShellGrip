package session

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// applyCustomPrompt rewrites argv and env for supported interactive shells so
// they load ShellGrip's themed prompt and OSC 133 semantic prompt markers after
// the user's normal rc files. Unsupported shells and non-interactive commands
// are returned unchanged.
func applyCustomPrompt(path string, argv []string, name string, env []string) ([]string, []string) {
	shell := strings.ToLower(strings.TrimSuffix(filepath.Base(name), ".exe"))
	switch shell {
	case "pwsh", "powershell":
		// shellCommand on Windows appends "-NoExit -Command <powershellPrompt>".
		// Replace that built-in wrapper with ShellGrip's custom prompt; leave any
		// user-supplied -Command/-File invocations untouched.
		out := slices.Clone(argv)
		if len(out) >= 4 && strings.EqualFold(out[len(out)-3], "-NoExit") && strings.EqualFold(out[len(out)-2], "-Command") && strings.Contains(out[len(out)-1], "$global:__ShellGripPrompt") {
			out[len(out)-1] = pwshPromptScript
			return out, env
		}
		if hasAnyFlag(out[1:], "-command", "-c", "-file", "-f", "-encodedcommand", "-e", "-ec") {
			return argv, env
		}
		out = append(out, "-NoExit", "-Command", pwshPromptScript)
		return out, env

	case "bash":
		if hasAnyFlag(argv[1:], "-c", "--rcfile", "--init-file") {
			return argv, env
		}
		script, err := writePromptFile("shellgrip-prompt.bash", bashPromptScript)
		if err != nil {
			return argv, env
		}
		// Bash ignores --rcfile when started as a login shell (--login / -l / -bash).
		// Strip login flags here and source login profiles inside the rcfile instead.
		out := make([]string, 0, len(argv)+3)
		if len(argv) > 0 {
			out = append(out, strings.TrimPrefix(argv[0], "-"))
		} else {
			out = append(out, path)
		}
		for _, arg := range argv[1:] {
			if arg == "--login" || arg == "-l" {
				continue
			}
			out = append(out, arg)
		}
		out = append(out, "--rcfile", filepath.ToSlash(script))
		if !slices.Contains(out[1:], "-i") {
			out = append(out, "-i")
		}
		return out, env

	case "zsh":
		if hasAnyFlag(argv[1:], "-c") {
			return argv, env
		}
		dir, err := writeZshPromptDir()
		if err != nil {
			return argv, env
		}
		return argv, withZdotdir(env, dir)

	case "fish":
		if hasAnyFlag(argv[1:], "-c", "--command") {
			return argv, env
		}
		script, err := writePromptFile("shellgrip-prompt.fish", fishPromptScript)
		if err != nil {
			return argv, env
		}
		out := slices.Clone(argv)
		out = append(out, "-C", "source '"+strings.ReplaceAll(script, "'", "\\'")+"'")
		return out, env

	case "wsl":
		if hasAnyFlag(argv[1:], "--", "-e", "--exec") {
			return argv, env
		}
		out := slices.Clone(argv)
		out = append(out, "--exec", "sh", "-c", wslPromptBootstrap())
		return out, env

	default:
		return argv, env
	}
}

func wslPromptBootstrap() string {
	b64Bash := base64.StdEncoding.EncodeToString([]byte(bashPromptScript))
	b64Zsh := base64.StdEncoding.EncodeToString([]byte(zshPromptScript))
	b64Fish := base64.StdEncoding.EncodeToString([]byte(fishPromptScript))
	return fmt.Sprintf(
		`sh=$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f7); `+
			`[ -n "$sh" ] || sh="${SHELL:-/bin/bash}"; `+
			`export SHELL="$sh"; `+
			`base="${sh##*/}"; `+
			`dir="/tmp/shellgrip-prompt-$(id -u)"; `+
			`mkdir -p "$dir" && chmod 700 "$dir" 2>/dev/null || true; `+
			`case "$base" in `+
			`bash) printf '%%s' '%s' | base64 -d > "$dir/shellgrip-prompt.bash" && exec "$sh" --rcfile "$dir/shellgrip-prompt.bash" -i ;; `+
			`zsh) mkdir -p "$dir/zsh" && printf '%%s' '%s' | base64 -d > "$dir/zsh/.zshenv" && [ -n "${ZDOTDIR+x}" ] && export SHELLGRIP_ORIG_ZDOTDIR="$ZDOTDIR"; export ZDOTDIR="$dir/zsh"; exec "$sh" -l ;; `+
			`fish) printf '%%s' '%s' | base64 -d > "$dir/shellgrip-prompt.fish" && exec "$sh" -l -C "source '$dir/shellgrip-prompt.fish'" ;; `+
			`*) exec "$sh" -l ;; `+
			`esac`,
		b64Bash, b64Zsh, b64Fish,
	)
}

func hasAnyFlag(args []string, flags ...string) bool {
	for _, arg := range args {
		low := strings.ToLower(arg)
		if slices.Contains(flags, low) {
			return true
		}
	}
	return false
}

func promptDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "shellgrip-prompt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func writePromptFile(name, content string) (string, error) {
	dir, err := promptDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

func writeZshPromptDir() (string, error) {
	base, err := promptDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "zsh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ".zshenv"), []byte(zshPromptScript), 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

func withZdotdir(env []string, dir string) []string {
	clean := make([]string, 0, len(env)+2)
	var orig string
	var hadOrig bool
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, "ZDOTDIR="); ok {
			orig, hadOrig = v, true
		} else if !strings.HasPrefix(entry, "SHELLGRIP_ORIG_ZDOTDIR=") {
			clean = append(clean, entry)
		}
	}
	if hadOrig {
		clean = append(clean, "SHELLGRIP_ORIG_ZDOTDIR="+orig)
	}
	return append(clean, "ZDOTDIR="+dir)
}

const pwshPromptScript = `$script:__shellgrip_first_prompt = $true; ` +
	`function global:prompt { ` +
	`$ok = $global:?; ` +
	`$code = if ($ok) { 0 } elseif ($null -ne $global:LASTEXITCODE -and $global:LASTEXITCODE -ne 0) { $global:LASTEXITCODE } else { 1 }; ` +
	`$e = [char]27; $bel = [char]7; ` +
	`try { $l = $executionContext.SessionState.Path.CurrentLocation; if ($l.Provider.Name -eq 'FileSystem') { [Environment]::CurrentDirectory = $l.ProviderPath; [Console]::Write("${e}]7;" + ([uri]$l.ProviderPath).AbsoluteUri + "${bel}") } } catch {}; ` +
	`$out = ""; ` +
	`if (-not $script:__shellgrip_first_prompt) { $out += "${e}]133;D;${code}${bel}` + "`n" + `" }; ` +
	`$script:__shellgrip_first_prompt = $false; ` +
	`$out += "${e}]133;A${bel}"; ` +
	`$cwd = (Get-Location).ProviderPath; ` +
	`if ($HOME -and $cwd.StartsWith($HOME, [System.StringComparison]::OrdinalIgnoreCase)) { $cwd = "~" + $cwd.Substring($HOME.Length) }; ` +
	`$branch = ""; ` +
	`if (Get-Command git -ErrorAction SilentlyContinue) { $b = git rev-parse --abbrev-ref HEAD 2>$null; if ($LASTEXITCODE -eq 0 -and $b) { $dirty = ""; if (git status --porcelain --ignore-submodules=dirty 2>$null | Select-Object -First 1) { $dirty = " ${e}[33m*${e}[0m" }; $branch = " ${e}[35m ${b}${e}[0m${dirty}" } }; ` +
	`$envBadge = ""; ` +
	`$pyEnv = if ($env:VIRTUAL_ENV) { Split-Path -Leaf $env:VIRTUAL_ENV } elseif ($env:CONDA_DEFAULT_ENV) { $env:CONDA_DEFAULT_ENV } else { "" }; ` +
	`if ($pyEnv) { $envBadge = " ${e}[33m ${pyEnv}${e}[0m" }; ` +
	`$errBadge = if ($code -ne 0) { " ${e}[31m[${code}]${e}[0m" } else { "" }; ` +
	`$arrowColor = if ($code -eq 0) { "${e}[1;32m" } else { "${e}[1;31m" }; ` +
	`$out += "${e}[1;34m${cwd}${e}[0m${branch}${envBadge}${errBadge}` + "`n" + `${arrowColor}❯${e}[0m ${e}]133;B${bel}"; ` +
	`return $out }`

const bashPromptScript = `if [ -r /etc/profile ]; then . /etc/profile; fi
if [ -r "$HOME/.bash_profile" ]; then
  . "$HOME/.bash_profile"
elif [ -r "$HOME/.bash_login" ]; then
  . "$HOME/.bash_login"
elif [ -r "$HOME/.profile" ]; then
  . "$HOME/.profile"
fi
if [ -r "$HOME/.bashrc" ]; then . "$HOME/.bashrc"; fi

__shellgrip_first_prompt=1
__shellgrip_in_cmd=0
__shellgrip_preexec() {
  if [ "$__shellgrip_in_cmd" = "0" ]; then
    __shellgrip_in_cmd=1
    printf '\e]133;C\a'
  fi
}
trap '__shellgrip_preexec' DEBUG

__shellgrip_prompt_cmd() {
  local code=$?
  if [ "$__shellgrip_in_cmd" = "1" ]; then
    printf '\e]133;D;%d\a' "$code"
  fi
  __shellgrip_in_cmd=0

  local nl=""
  if [ "$__shellgrip_first_prompt" = "0" ]; then
    nl='\n'
  fi
  __shellgrip_first_prompt=0

  local branch=""
  if command -v git >/dev/null 2>&1; then
    local b
    b=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
    if [ -n "$b" ]; then
      local dirty=""
      if [ -n "$(git status --porcelain --ignore-submodules=dirty 2>/dev/null | head -n 1)" ]; then
        dirty=' \[\e[33m\]*\[\e[0m\]'
      fi
      branch=' \[\e[35m\] '"${b}"'\[\e[0m\]'"${dirty}"
    fi
  fi

  local py_env=""
  if [ -n "$VIRTUAL_ENV" ]; then
    py_env=' \[\e[33m\] '"${VIRTUAL_ENV##*/}"'\[\e[0m\]'
  elif [ -n "$CONDA_DEFAULT_ENV" ]; then
    py_env=' \[\e[33m\] '"${CONDA_DEFAULT_ENV}"'\[\e[0m\]'
  fi

  local err_badge=""
  local arrow='\[\e[1;32m\]❯\[\e[0m\]'
  if [ "$code" -ne 0 ]; then
    err_badge=' \[\e[31m\]['"${code}"']\[\e[0m\]'
    arrow='\[\e[1;31m\]❯\[\e[0m\]'
  fi
  PS1="${nl}"'\[\e]133;A\a\]\[\e[1;34m\]\w\[\e[0m\]'"${branch}${py_env}${err_badge}"'\n'"${arrow}"' \[\e]133;B\a\]'
}
PROMPT_COMMAND=__shellgrip_prompt_cmd
`

const zshPromptScript = `if (( ${+SHELLGRIP_ORIG_ZDOTDIR} )); then
  export ZDOTDIR=$SHELLGRIP_ORIG_ZDOTDIR
  unset SHELLGRIP_ORIG_ZDOTDIR
else
  unset ZDOTDIR
fi
[[ ! -r ${ZDOTDIR-$HOME}/.zshenv ]] || source -- "${ZDOTDIR-$HOME}/.zshenv"
[[ -o interactive ]] || return

typeset -gi __shellgrip_first_prompt=1

_shellgrip_precmd() {
  local code=$?
  emulate -L zsh
  if ! zle; then
    printf '\e]133;D;%d\a' $code
  fi
  local nl=""
  if (( __shellgrip_first_prompt == 0 )); then
    nl=$'\n'
  fi
  __shellgrip_first_prompt=0
  local branch=""
  if (( $+commands[git] )); then
    local b
    b=$(git rev-parse --abbrev-ref HEAD 2>/dev/null)
    if [[ -n $b ]]; then
      local dirty=""
      if [[ -n $(git status --porcelain --ignore-submodules=dirty 2>/dev/null | head -n 1) ]]; then
        dirty=" %F{yellow}*%f"
      fi
      branch=" %F{magenta} ${b}%f${dirty}"
    fi
  fi
  local py_env=""
  if [[ -n $VIRTUAL_ENV ]]; then
    py_env=" %F{yellow} ${VIRTUAL_ENV:t}%f"
  elif [[ -n $CONDA_DEFAULT_ENV ]]; then
    py_env=" %F{yellow} ${CONDA_DEFAULT_ENV}%f"
  fi
  local err_badge=""
  local arrow="%B%F{green}❯%f%b"
  if (( code != 0 )); then
    err_badge=" %F{red}[${code}]%f"
    arrow="%B%F{red}❯%f%b"
  fi
  RPROMPT=""
  PROMPT="${nl}"$'%{\e]133;A;cl=line;redraw=1\a%}%B%F{blue}%~%f%b'"${branch}${py_env}${err_badge}"$'\n'"${arrow}"$' %{\e]133;B\a%}'
}
_shellgrip_preexec() {
  printf '\e]133;C\a'
}
_shellgrip_install_prompt() {
  emulate -L zsh
  autoload -Uz add-zsh-hook
  add-zsh-hook -d precmd _shellgrip_install_prompt
  add-zsh-hook precmd _shellgrip_precmd
  add-zsh-hook preexec _shellgrip_preexec
  _shellgrip_precmd
}
typeset -ga precmd_functions
precmd_functions+=(_shellgrip_install_prompt)
`

const fishPromptScript = `set -g __shellgrip_first_prompt 1

function __shellgrip_preexec --on-event fish_preexec
    printf '\e]133;C\a'
end

function fish_prompt
    set -l code $status
    printf '\e]133;D;%d\a' $code
    if test $__shellgrip_first_prompt -eq 0
        printf '\n'
    end
    set -g __shellgrip_first_prompt 0
    printf '\e]133;A\a'
    set -l cwd (prompt_pwd)
    set -l branch ""
    if type -q git
        set -l b (git rev-parse --abbrev-ref HEAD 2>/dev/null)
        if test -n "$b"
            set -l dirty ""
            if test -n (git status --porcelain --ignore-submodules=dirty 2>/dev/null | head -n 1 | string collect)
                set dirty (printf ' \e[33m*\e[0m')
            end
            set branch (printf ' \e[35m %s\e[0m%s' "$b" "$dirty")
        end
    end
    set -l py_env ""
    if test -n "$VIRTUAL_ENV"
        set py_env (printf ' \e[33m %s\e[0m' (basename "$VIRTUAL_ENV"))
    else if test -n "$CONDA_DEFAULT_ENV"
        set py_env (printf ' \e[33m %s\e[0m' "$CONDA_DEFAULT_ENV")
    end
    set -l err_badge ""
    set -l arrow (printf '\e[1;32m❯\e[0m')
    if test $code -ne 0
        set err_badge (printf ' \e[31m[%d]\e[0m' $code)
        set arrow (printf '\e[1;31m❯\e[0m')
    end
    printf '\e[1;34m%s\e[0m%s%s%s\n%s \e]133;B\a' "$cwd" "$branch" "$py_env" "$err_badge" "$arrow"
end
`
