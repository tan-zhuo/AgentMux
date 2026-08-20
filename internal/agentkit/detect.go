package agentkit

import (
	"fmt"
	"strings"

	"agentmux/internal/sshx"
)

// Runner executes a command on a server. *sshx.Pool satisfies it.
type Runner interface {
	Exec(serverID, cmd string) (sshx.ExecResult, error)
}

const fieldSep = "~@~"

// pathPrelude puts the places agent CLIs actually install themselves onto PATH.
//
// An SSH exec channel runs a non-login, non-interactive shell, which reads
// neither ~/.profile nor the part of ~/.bashrc that most distributions guard
// behind an interactivity check. So the PATH we inherit is the bare system one,
// and a perfectly working `claude` in ~/.local/bin is invisible — the tool
// reports "not installed" for something the user runs by hand every day.
//
// Listing the locations explicitly is deliberate. Sourcing the user's profile
// would be more general but runs arbitrary code in a shell it was not written
// for, and one `exit` or a bash-ism in a POSIX sh would take the whole probe
// down with it.
const pathPrelude = `
for d in "$HOME/.local/bin" "$HOME/bin" "$HOME/.claude/local" "$HOME/.grok/bin" \
         "$HOME/.opencode/bin" \
         "$HOME/.npm-global/bin" "$HOME/.node_modules/bin" "$HOME/.yarn/bin" \
         "$HOME/.bun/bin" "$HOME/.deno/bin" "$HOME/.cargo/bin" "$HOME/go/bin" \
         "$HOME/.volta/bin" "$HOME/.pyenv/shims" "$HOME/.rye/shims" \
         "/opt/homebrew/bin" "/home/linuxbrew/.linuxbrew/bin" \
         "/usr/local/bin" "/snap/bin"; do
  [ -d "$d" ] && PATH="$d:$PATH"
done
for d in "$HOME/.nvm/versions/node"/*/bin \
         "$HOME/.local/share/fnm/node-versions"/*/installation/bin \
         "$HOME/.asdf/shims"; do
  [ -d "$d" ] && PATH="$d:$PATH"
done
export PATH
`

// PathPrelude exposes the search-path setup to callers that build their own
// probe commands, so there is one list to keep current rather than several.
func PathPrelude() string { return pathPrelude }

// Presence is what one probed binary looks like on a server.
type Presence struct {
	Binary    string `json:"binary"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
}

// ToolStatus pairs a catalogue entry with what was found on the server.
type ToolStatus struct {
	Tool      Tool   `json:"tool"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	// Methods that are actually usable here, in preference order.
	Available []Method `json:"available"`
	// Blocked explains why nothing is installable when Available is empty.
	Blocked string `json:"blocked"`
}

// Report is the full toolchain picture for one server.
type Report struct {
	ServerID string              `json:"serverId"`
	OS       string              `json:"os"`
	Shell    string              `json:"shell"`
	Agents   []ToolStatus        `json:"agents"`
	Runtimes []ToolStatus        `json:"runtimes"`
	Presence map[string]Presence `json:"presence"`
	Error    string              `json:"error"`
}

// Detect probes every catalogue binary in a single round trip. Doing this as
// one script rather than one exec per tool keeps a fifty-server sweep cheap.
func Detect(run Runner, serverID string) Report {
	rep := Report{ServerID: serverID, Presence: map[string]Presence{}}

	binaries := probeBinaries()
	var script strings.Builder
	script.WriteString(pathPrelude)
	script.WriteString(`printf 'os` + fieldSep + `%s\n' "$(uname -sr 2>/dev/null || echo unknown)"; `)
	script.WriteString(`printf 'shell` + fieldSep + `%s\n' "${SHELL:-unknown}"; `)
	for _, b := range binaries {
		q := sshx.ShellQuote(b)
		// One line per binary: bin<sep>name<sep>path. Version is read separately
		// only for what is present, to keep this pass fast.
		script.WriteString(fmt.Sprintf(
			`if p=$(command -v %s 2>/dev/null); then printf 'bin`+fieldSep+`%%s`+fieldSep+`%%s\n' %s "$p"; fi; `,
			q, q))
	}

	res, err := run.Exec(serverID, script.String())
	if err != nil {
		rep.Error = err.Error()
		return rep
	}

	for _, line := range strings.Split(strings.ReplaceAll(res.Stdout, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), fieldSep, 3)
		switch {
		case len(parts) == 2 && parts[0] == "os":
			rep.OS = parts[1]
		case len(parts) == 2 && parts[0] == "shell":
			rep.Shell = parts[1]
		case len(parts) == 3 && parts[0] == "bin":
			rep.Presence[parts[1]] = Presence{Binary: parts[1], Installed: true, Path: parts[2]}
		}
	}
	for _, b := range binaries {
		if _, ok := rep.Presence[b]; !ok {
			rep.Presence[b] = Presence{Binary: b}
		}
	}

	// Version strings for the installed tools, again in one round trip.
	versions := versionsFor(run, serverID, rep.Presence)

	for _, t := range All() {
		st := ToolStatus{Tool: t}
		p := rep.Presence[t.Binary]
		st.Installed = p.Installed
		st.Path = p.Path
		st.Version = versions[t.Binary]
		for _, m := range t.Methods {
			if m.Requires == "" || rep.Presence[m.Requires].Installed {
				st.Available = append(st.Available, m)
			}
		}
		if len(st.Available) == 0 {
			st.Blocked = blockedReason(t)
		}
		if t.Kind == "runtime" {
			rep.Runtimes = append(rep.Runtimes, st)
		} else {
			rep.Agents = append(rep.Agents, st)
		}
	}
	return rep
}

func versionsFor(run Runner, serverID string, presence map[string]Presence) map[string]string {
	var script strings.Builder
	script.WriteString(pathPrelude)
	wanted := 0
	for _, t := range All() {
		if !presence[t.Binary].Installed || t.VersionArgs == "" {
			continue
		}
		wanted++
		script.WriteString(fmt.Sprintf(
			`printf '%%s`+fieldSep+`%%s\n' %s "$(%s %s 2>/dev/null | head -1 | tr -d '\r')"; `,
			sshx.ShellQuote(t.Binary), t.Binary, t.VersionArgs))
	}
	out := map[string]string{}
	if wanted == 0 {
		return out
	}
	res, err := run.Exec(serverID, script.String())
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.ReplaceAll(res.Stdout, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(strings.TrimRight(line, "\r"), fieldSep, 2)
		if len(parts) == 2 && parts[1] != "" {
			out[parts[0]] = strings.TrimSpace(parts[1])
		}
	}
	return out
}

func blockedReason(t Tool) string {
	missing := make([]string, 0, len(t.Methods))
	for _, m := range t.Methods {
		if m.Requires != "" {
			missing = append(missing, m.Requires)
		}
	}
	if len(missing) == 0 {
		return "no install method available on this server"
	}
	return "needs one of: " + strings.Join(dedupe(missing), ", ")
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
