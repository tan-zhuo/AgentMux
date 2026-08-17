package memory

import "regexp"

// Memories are harvested from remote logs and pane output, which means they
// will eventually contain a token, a private key or a connection string. Once
// one is in the library it goes into the model's context on every retrieval
// that matches — the leak is not "someone stole the database file", it is the
// same secret being handed to a program over and over.
//
// So secrets are removed on the way in. The rules below are deliberately blunt:
// a false positive costs one unreadable fragment of a log line, a false
// negative costs a credential.

type rule struct {
	kind string
	re   *regexp.Regexp
}

var rules = []rule{
	// PEM blocks, including the body, which is the part that matters.
	{"private-key", regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{"github-token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
	{"openai-key", regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)},
	{"anthropic-key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)},
	{"aws-key-id", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"slack-token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"jwt", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)},
	{"bearer", regexp.MustCompile(`(?i)\b(authorization\s*:\s*bearer|bearer)\s+[A-Za-z0-9._~+/=-]{12,}`)},
	// key=value forms, in shell exports, URLs and config lines alike.
	//
	// The key may carry a prefix, because the real ones almost always do:
	// PGPASSWORD, DB_PASSWORD, CSRF_TOKEN. Anchoring on a word boundary — the
	// obvious way to write this — matched none of them.
	//
	// Bare "pwd" is excluded and only recognised after a separator, so that
	// MYSQL_PWD is caught while PWD=/home/you, which appears in the environment
	// of every shell command anyone will ever remember, is left alone.
	{"secret-assignment", regexp.MustCompile(
		`(?i)((?:[A-Za-z0-9_]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key))|(?:[A-Za-z0-9]+[_-]pwd))(\s*[:=]\s*)("[^"]*"|'[^']*'|\S+)`)},
	// Credentials embedded in a URL: scheme://user:pass@host
	{"url-credentials", regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]+):([^\s@/]+)@`)},
}

// Redact removes anything that looks like a credential. It reports whether it
// changed the text so the memory can be marked, which is the difference between
// "this log had nothing sensitive" and "we cleaned it".
func Redact(s string) (string, bool) {
	out := s
	for _, r := range rules {
		switch r.kind {
		case "secret-assignment":
			// Keep the key and the separator, drop only the value:
			// "PGPASSWORD=[REDACTED:secret]" still tells a reader what kind of
			// line this was.
			out = r.re.ReplaceAllString(out, "$1$2[REDACTED:secret]")
		case "url-credentials":
			out = r.re.ReplaceAllString(out, "$1:[REDACTED:password]@")
		default:
			out = r.re.ReplaceAllString(out, "[REDACTED:"+r.kind+"]")
		}
	}
	return out, out != s
}
