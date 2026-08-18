package agentkit

// Detection for hosts whose shell is PowerShell — the native Windows local
// host. This file has no build tag on purpose: which probe to run is a property
// of the host being asked, not of the platform this binary runs on.

import (
	"fmt"
	"strings"
)

// DetectWindows probes the catalogue through PowerShell, in the same two round
// trips and the same Report shape as the POSIX probe.
func DetectWindows(run Runner, serverID string) Report {
	rep := Report{ServerID: serverID, Presence: map[string]Presence{}}

	binaries := probeBinaries()
	var script strings.Builder
	script.WriteString(`$ErrorActionPreference='SilentlyContinue'; `)
	script.WriteString(`'os` + fieldSep + `' + (Get-CimInstance Win32_OperatingSystem).Caption; `)
	script.WriteString(`'shell` + fieldSep + `powershell'; `)
	for _, b := range binaries {
		q := psQuote(b)
		// Get-Command sees .exe, .cmd and .ps1 shims alike, which matters here:
		// the npm-installed agents land on Windows as .cmd files.
		script.WriteString(fmt.Sprintf(
			`$p=(Get-Command %s -ErrorAction SilentlyContinue).Source; if($p){'bin`+fieldSep+`'+%s+'`+fieldSep+`'+$p}; `,
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
			rep.OS = strings.TrimSpace(parts[1])
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

	versions := windowsVersionsFor(run, serverID, rep.Presence)

	for _, t := range All() {
		// tmux has no meaning on this host: sessions persist through AgentMux's
		// own daemon, so there is nothing to install and nothing to miss.
		if t.ID == "tmux" {
			continue
		}
		st := ToolStatus{Tool: t}
		p := rep.Presence[t.Binary]
		st.Installed = p.Installed
		st.Path = p.Path
		st.Version = versions[t.Binary]
		for _, m := range t.Methods {
			if !methodWorksOnWindows(m) {
				continue
			}
			if m.Requires == "" || rep.Presence[m.Requires].Installed {
				st.Available = append(st.Available, m)
			}
		}
		if len(st.Available) == 0 {
			st.Blocked = blockedReasonWindows(t)
		}
		if t.Kind == "runtime" {
			rep.Runtimes = append(rep.Runtimes, st)
		} else {
			rep.Agents = append(rep.Agents, st)
		}
	}
	return rep
}

func windowsVersionsFor(run Runner, serverID string, presence map[string]Presence) map[string]string {
	var script strings.Builder
	script.WriteString(`$ErrorActionPreference='SilentlyContinue'; `)
	wanted := 0
	for _, t := range All() {
		if !presence[t.Binary].Installed || t.VersionArgs == "" {
			continue
		}
		wanted++
		script.WriteString(fmt.Sprintf(
			`$v = (& %s %s 2>$null | Select-Object -First 1); if($v){'%s`+fieldSep+`'+$v}; `,
			psQuote(t.Binary), t.VersionArgs, t.Binary))
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

// VerifyWindows re-probes one binary through PowerShell after an install.
func VerifyWindows(run Runner, serverID string, tool Tool) (Presence, error) {
	script := fmt.Sprintf(
		`$ErrorActionPreference='SilentlyContinue'; `+
			`$c = Get-Command %s -ErrorAction SilentlyContinue; `+
			`if ($c) { $c.Source; & %s %s 2>$null | Select-Object -First 1 }`,
		psQuote(tool.Binary), psQuote(tool.Binary), tool.VersionArgs)
	res, err := run.Exec(serverID, script)
	if err != nil {
		return Presence{Binary: tool.Binary}, err
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(res.Stdout, "\r\n", "\n")), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Presence{Binary: tool.Binary}, nil
	}
	p := Presence{Binary: tool.Binary, Installed: true, Path: strings.TrimSpace(lines[0])}
	if len(lines) > 1 {
		p.Version = strings.TrimSpace(lines[1])
	}
	return p, nil
}

// methodWorksOnWindows filters the catalogue's install scripts down to the ones
// PowerShell can actually run. The scripts are written for a POSIX sh; the
// package-manager one-liners (npm, pipx) happen to be dialect-free, while
// anything piping an installer into bash, chaining with && or asking sudo is
// not an install method here — it is a way to print errors.
func methodWorksOnWindows(m Method) bool {
	s := m.Script
	if strings.Contains(s, "| bash") || strings.Contains(s, "| sh") ||
		strings.Contains(s, "&&") || strings.Contains(s, "sudo ") ||
		strings.Contains(s, "export ") {
		return false
	}
	return true
}

func blockedReasonWindows(t Tool) string {
	missing := make([]string, 0, len(t.Methods))
	for _, m := range t.Methods {
		if methodWorksOnWindows(m) && m.Requires != "" {
			missing = append(missing, m.Requires)
		}
	}
	if len(missing) == 0 {
		return "no install method works on native Windows; install it by hand and re-detect"
	}
	return "needs one of: " + strings.Join(dedupe(missing), ", ")
}

// psQuote wraps a string in PowerShell single quotes.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
