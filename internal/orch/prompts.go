package orch

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"text/template"
	"time"
)

//go:embed prompts/*.md
var promptFS embed.FS

var prompts = template.Must(template.ParseFS(promptFS, "prompts/*.md"))

func render(name string, data any) (string, error) {
	var b strings.Builder
	if err := prompts.ExecuteTemplate(&b, name, data); err != nil {
		return "", fmt.Errorf("rendering %s: %w", name, err)
	}
	return b.String(), nil
}

// planData is what the planning prompt is built from.
type planData struct {
	Goal        string
	Observation string
	Memories    []promptMemory
	Skills      []promptSkill
}

type promptMemory struct {
	Kind string
	Text string
}

type promptSkill struct {
	Name        string
	Trigger     string
	Score       float32
	Steps       []promptStep
	Constraints []string
}

type promptStep struct {
	Order       int
	Description string
	Tools       string
}

// observationData wraps untrusted text.
type observationData struct {
	Nonce   string
	Source  string
	Where   string
	At      string
	Content string
}

// wrapObserved renders a block of untrusted text with boundaries the source
// cannot forge.
//
// The nonce is why the marker is unguessable: without it, text that itself
// contains "END OBSERVED DATA" could close the block early and have everything
// after it read as instructions from the orchestrator rather than as data.
func wrapObserved(source, where, content string) (string, error) {
	return render("observation.md", observationData{
		Nonce:   nonce(),
		Source:  source,
		Where:   where,
		At:      time.Now().Format(time.RFC3339),
		Content: content,
	})
}

func nonce() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// A predictable marker is worse than a slow one, but this cannot
		// realistically fail; the timestamp keeps it distinct if it ever does.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// reflectData is what the reflection prompt is built from.
type reflectData struct {
	Goal    string
	Steps   []reflectStep
	Outcome string
	Similar []reflectSimilar
}

type reflectStep struct {
	Seq       int
	Tool      string
	Reasoning string
	Result    string
}

type reflectSimilar struct {
	Name        string
	Description string
	Score       float32
}
