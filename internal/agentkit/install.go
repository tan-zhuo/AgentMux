package agentkit

import "strings"

// npmPrefixGuard makes `npm install -g` work for a non-root user whose node
// came from a system package. There npm's global prefix is /usr, the install
// dies with EACCES every single time, and nothing in the output suggests the
// one-line fix. Switching the prefix to ~/.npm-global — only when the current
// prefix is genuinely unwritable — turns that guaranteed failure into a
// working install, and the PATH line goes into ~/.profile so the binary is
// still there on the next login.
const npmPrefixGuard = `
if [ "$(id -u)" != "0" ] && command -v npm >/dev/null 2>&1; then
  np=$(npm config get prefix 2>/dev/null)
  case "$np" in
    "$HOME"*) : ;;
    "")       : ;;
    *)
      if [ ! -w "$np/lib/node_modules" ] && [ ! -w "$np/lib" ] && [ ! -w "$np" ]; then
        echo "=== AgentMux: npm prefix $np is not writable, using $HOME/.npm-global instead ==="
        mkdir -p "$HOME/.npm-global/bin"
        npm config set prefix "$HOME/.npm-global"
        PATH="$HOME/.npm-global/bin:$PATH"; export PATH
        grep -qs 'npm-global/bin' "$HOME/.profile" 2>/dev/null || \
          printf '\nexport PATH="$HOME/.npm-global/bin:$PATH"\n' >> "$HOME/.profile"
      fi
      ;;
  esac
fi
`

// npmMirror is retried against when a global npm install fails. The default
// registry is unreachable or glacial from servers in some regions — mainland
// China above all — where a fresh box otherwise fails every install with a
// bare timeout.
const npmMirror = "https://registry.npmmirror.com"

// InstallScript wraps one catalogue install command into the script actually
// run on the server. The wrapping exists because a fresh server fails the bare
// vendor command in predictable ways:
//
//   - the shell the install runs in never read the profile, so a runtime
//     installed minutes earlier into $HOME is not on PATH — the same trap
//     detection already works around, so the same prelude is applied;
//   - `npm install -g` needs root when node came from a system package;
//   - the default npm registry is unreachable from some networks.
//
// The script ends by reporting success or the exit code in a banner and then
// exec'ing a login shell, so the outcome is readable in the pane and the
// window survives to show it.
func InstallScript(label, script string, viaNpm bool) string {
	var b strings.Builder
	b.WriteString(pathPrelude)
	if viaNpm {
		b.WriteString(npmPrefixGuard)
	}
	l := bannerSafe(label)
	b.WriteString("echo '=== AgentMux: installing " + l + " ==='\n")
	b.WriteString("(\n" + script + "\n)\nrc=$?\n")
	if viaNpm {
		b.WriteString("if [ $rc -ne 0 ]; then\n")
		b.WriteString("  echo '=== AgentMux: install failed, retrying via " + npmMirror + " ==='\n")
		b.WriteString("  (\n" + script + " --registry=" + npmMirror + "\n)\n  rc=$?\nfi\n")
	}
	b.WriteString(`if [ $rc -eq 0 ]; then
  echo '=== AgentMux: ` + l + ` — install finished OK ==='
else
  echo '=== AgentMux: ` + l + ` — install FAILED (exit '"$rc"') ==='
fi
exec "${SHELL:-/bin/sh}" -l
`)
	return b.String()
}

// bannerSafe strips what would break the single-quoted echo banners: quotes,
// backslashes and control characters. Labels are catalogue data except for the
// custom-script path, where the user types them.
func bannerSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\'' || r == '\\' || r < ' ' {
			return -1
		}
		return r
	}, s)
}
