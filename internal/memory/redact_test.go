package memory_test

import (
	"strings"
	"testing"

	"agentmux/internal/memory"
)

func TestRedactRemovesCredentials(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		secret string
	}{
		{"github token", "cloned with ghp_0123456789abcdefghijKLMNOP", "ghp_0123456789abcdefghijKLMNOP"},
		{"openai key", "OPENAI_KEY sk-abcdefghijklmnopqrstuvwxyz0123", "sk-abcdefghijklmnopqrstuvwxyz0123"},
		{"aws key id", "user AKIAIOSFODNN7EXAMPLE denied", "AKIAIOSFODNN7EXAMPLE"},
		{"bearer header", "Authorization: Bearer eyAbcdefghijklmnop.qrst", "eyAbcdefghijklmnop.qrst"},
		{"password assignment", `env PGPASSWORD=hunter2correct`, "hunter2correct"},
		{"prefixed key", `DB_PASSWORD=swordfish123`, "swordfish123"},
		{"pwd after separator", `MYSQL_PWD=rootpass99`, "rootpass99"},
		{"quoted secret", `api_key: "s3cr3t-value-here"`, "s3cr3t-value-here"},
		{"url credentials", "psql postgres://admin:letmein@db.internal/app", "letmein"},
		{"private key", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA\n-----END OPENSSH PRIVATE KEY-----", "b3BlbnNzaA"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, changed := memory.Redact(c.in)
			if !changed {
				t.Fatalf("nothing was redacted in %q", c.in)
			}
			if strings.Contains(out, c.secret) {
				t.Errorf("the secret survived: %q", out)
			}
			if !strings.Contains(out, "REDACTED") {
				t.Errorf("the redaction left no marker: %q", out)
			}
		})
	}
}

func TestRedactKeepsOrdinaryText(t *testing.T) {
	// False positives cost readability on every log line that gets remembered,
	// so ordinary prose containing these words must come through untouched.
	ordinary := []string{
		"the deploy failed because the disk was full",
		"agent restarted after a timeout of 30s",
		"password reset email was sent to the user",
		"see the token bucket rate limiter in rate.go",
		// PWD is in the environment of every command anyone will ever remember.
		// Treating it as a secret would redact the working directory out of
		// most of the library.
		"PWD=/home/you/src/agentmux make build",
	}
	for _, s := range ordinary {
		out, changed := memory.Redact(s)
		if changed {
			t.Errorf("redacted ordinary text %q -> %q", s, out)
		}
	}
}

func TestRedactKeepsTheKeyNameVisible(t *testing.T) {
	// "password=[REDACTED]" tells a reader what kind of line this was;
	// replacing the whole line would lose that.
	out, _ := memory.Redact("PGPASSWORD=hunter2correct psql -h db")
	if !strings.Contains(out, "PGPASSWORD") {
		t.Errorf("the key name should survive: %q", out)
	}
	if !strings.Contains(out, "psql -h db") {
		t.Errorf("the rest of the line should survive: %q", out)
	}
}
