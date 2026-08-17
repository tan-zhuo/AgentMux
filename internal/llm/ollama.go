// Package llm talks to a local Ollama instance.
//
// Everything here is deliberately unexciting: one HTTP client, no streaming, no
// third-party SDK. The orchestrator needs three things from a local model —
// a chat turn that can call tools, an embedding, and the list of what is
// installed — and each is one POST.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// DefaultBaseURL is where Ollama listens unless it was told otherwise.
const DefaultBaseURL = "http://127.0.0.1:11434"

// DefaultEmbedModel is bge-m3: 1024 dimensions, and noticeably better on
// Chinese text than the smaller English-first embedders. What gets embedded
// here is project knowledge and user preferences, and a retrieval miss reads to
// the user as the assistant simply forgetting things.
const DefaultEmbedModel = "bge-m3"

// DefaultChatModel is a mid-sized Qwen3, which fits on a normal machine and is
// competent at tool calling.
const DefaultChatModel = "qwen3:8b"

// Client is a handle on one Ollama server.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New builds a client. A blank base URL means the local default.
//
// The timeout is generous because these calls are not interactive latency: a
// cold model has to be loaded into memory first, and on a CPU-only machine
// embedding a batch legitimately takes tens of seconds. Per-call deadlines come
// from the context, which is what actually lets the UI cancel.
func New(baseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// Model is one installed model.
type Model struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modifiedAt"`
}

// Status is what the settings screen shows about the runtime.
type Status struct {
	BaseURL    string  `json:"baseUrl"`
	Reachable  bool    `json:"reachable"`
	Version    string  `json:"version"`
	Models     []Model `json:"models"`
	ChatModel  string  `json:"chatModel"`
	EmbedModel string  `json:"embedModel"`
	// ChatModelReady and EmbedModelReady say whether the configured names are
	// actually installed. "Connected, but the model you named is not pulled" is
	// the most common way this is broken, and it is invisible otherwise.
	ChatModelReady  bool   `json:"chatModelReady"`
	EmbedModelReady bool   `json:"embedModelReady"`
	Error           string `json:"error"`
	// Hint is a next action in plain words, not a stack trace.
	Hint string `json:"hint"`
}

// Probe reports whether Ollama is up and whether the configured models exist.
//
// It never returns an error: an unreachable runtime is a state the UI renders,
// not an exception. The distinction matters because Ollama being absent is the
// normal case for someone who has not set this up yet.
func (c *Client) Probe(ctx context.Context, chatModel, embedModel string) Status {
	st := Status{BaseURL: c.BaseURL, ChatModel: chatModel, EmbedModel: embedModel}

	var ver struct {
		Version string `json:"version"`
	}
	if err := c.get(ctx, "/api/version", &ver); err != nil {
		st.Error = err.Error()
		st.Hint = c.diagnose(err)
		return st
	}
	st.Reachable = true
	st.Version = ver.Version

	models, err := c.Models(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Models = models

	for _, m := range models {
		if modelMatches(m.Name, chatModel) {
			st.ChatModelReady = true
		}
		if modelMatches(m.Name, embedModel) {
			st.EmbedModelReady = true
		}
	}
	switch {
	case !st.EmbedModelReady && embedModel != "":
		st.Hint = fmt.Sprintf("Run `ollama pull %s` — memory search needs it.", embedModel)
	case !st.ChatModelReady && chatModel != "":
		st.Hint = fmt.Sprintf("Run `ollama pull %s` — planning needs it.", chatModel)
	}
	return st
}

// modelMatches compares a configured name against an installed one, treating a
// missing tag as ":latest" the way Ollama itself does. Without this, configuring
// "bge-m3" against an installed "bge-m3:latest" looks like a missing model.
func modelMatches(installed, want string) bool {
	if want == "" {
		return false
	}
	if installed == want {
		return true
	}
	if !strings.Contains(want, ":") && installed == want+":latest" {
		return true
	}
	return false
}

// diagnose turns a transport error into something a person can act on.
//
// The cases are recognised by error type rather than by matching the text of
// the message. The text is the operating system's, it differs between platforms
// and sandboxes, and a diagnosis that silently degrades to a shrug on some
// machines is worse than no diagnosis at all — the string match here used to do
// exactly that.
//
// The fallback is written to be worth reading too: whatever went wrong, if
// AgentMux cannot reach Ollama then "start Ollama" is the first thing to check,
// so the generic branch says so instead of restating that something failed.
func (c *Client) diagnose(err error) string {
	var dns *net.DNSError
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "Nothing is listening at " + c.BaseURL + ". Start Ollama with `ollama serve`."

	case errors.As(err, &dns):
		return "The host in " + c.BaseURL + " does not resolve. Check the address in Settings."

	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, os.ErrDeadlineExceeded),
		isTimeout(err):
		return "Ollama did not answer in time. It may still be loading a model."

	default:
		return "Could not reach Ollama at " + c.BaseURL +
			" (" + err.Error() + "). If it is not running, start it with `ollama serve`."
	}
}

// isTimeout covers the net.Error timeouts that are not one of the sentinel
// deadline errors.
func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Models lists what is installed.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var res struct {
		Models []struct {
			Name       string `json:"name"`
			Size       int64  `json:"size"`
			ModifiedAt string `json:"modified_at"`
		} `json:"models"`
	}
	if err := c.get(ctx, "/api/tags", &res); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(res.Models))
	for _, m := range res.Models {
		out = append(out, Model{Name: m.Name, Size: m.Size, ModifiedAt: m.ModifiedAt})
	}
	return out, nil
}

// Embed turns texts into vectors, in order.
//
// Two endpoints exist in the wild: /api/embed takes a batch and is what current
// Ollama serves, while older builds only have /api/embeddings and only one text
// at a time. Falling back keeps this working against whatever the user happens
// to have installed rather than demanding they upgrade first.
func (c *Client) Embed(ctx context.Context, model string, texts []string) ([][]float32, error) {
	if model == "" {
		return nil, errors.New("no embedding model configured")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	var res struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err := c.post(ctx, "/api/embed", map[string]any{"model": model, "input": texts}, &res)
	if err == nil {
		if len(res.Embeddings) != len(texts) {
			return nil, fmt.Errorf("asked for %d embeddings, got %d", len(texts), len(res.Embeddings))
		}
		return res.Embeddings, nil
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusNotFound {
		return nil, err
	}

	out := make([][]float32, len(texts))
	for i, t := range texts {
		var single struct {
			Embedding []float32 `json:"embedding"`
		}
		if err := c.post(ctx, "/api/embeddings",
			map[string]any{"model": model, "prompt": t}, &single); err != nil {
			return nil, err
		}
		if len(single.Embedding) == 0 {
			return nil, fmt.Errorf("model %s returned an empty embedding", model)
		}
		out[i] = single.Embedding
	}
	return out, nil
}

// --- chat -------------------------------------------------------------------

// Message is one turn of a conversation.
type Message struct {
	Role      string     `json:"role"` // system | user | assistant | tool
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	// ToolName ties a tool result back to the call that asked for it.
	ToolName string `json:"name,omitempty"`
}

// ToolCall is the model asking for a tool to be run.
type ToolCall struct {
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

// ToolDef describes a tool to the model.
type ToolDef struct {
	Type     string `json:"type"` // always "function"
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

// ChatRequest is one non-streaming chat turn.
type ChatRequest struct {
	Model    string
	Messages []Message
	Tools    []ToolDef
	// Format carries a JSON Schema when the answer has to be structured, which
	// is how a skill draft comes back parseable instead of prose-wrapped.
	Format json.RawMessage
	// Temperature is a pointer so that "not set" and "0" are different things;
	// 0 is a value the planner genuinely wants.
	Temperature *float64
}

// ChatResponse is what came back.
type ChatResponse struct {
	Message    Message `json:"message"`
	Model      string  `json:"model"`
	DoneReason string  `json:"done_reason"`
}

// Chat runs one turn. Streaming is off: the orchestrator acts on whole
// decisions, and a half-parsed tool call is not a decision.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if req.Model == "" {
		return ChatResponse{}, errors.New("no chat model configured")
	}
	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   false,
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if len(req.Format) > 0 {
		body["format"] = req.Format
	}
	if req.Temperature != nil {
		body["options"] = map[string]any{"temperature": *req.Temperature}
	}

	var res ChatResponse
	if err := c.post(ctx, "/api/chat", body, &res); err != nil {
		return ChatResponse{}, err
	}
	return res, nil
}

// --- transport --------------------------------------------------------------

// HTTPError is a non-2xx answer, kept typed so callers can branch on the code
// rather than matching on message text.
type HTTPError struct {
	Status int
	Body   string
	Path   string
}

func (e *HTTPError) Error() string {
	body := strings.TrimSpace(e.Body)
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	if body == "" {
		return fmt.Sprintf("%s: HTTP %d", e.Path, e.Status)
	}
	return fmt.Sprintf("%s: HTTP %d: %s", e.Path, e.Status, body)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, path, out)
}

func (c *Client) post(ctx context.Context, path string, payload, out any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, path, out)
}

func (c *Client) do(req *http.Request, path string, out any) error {
	res, err := c.HTTP.Do(req)
	if err != nil {
		// The URL is repeated in every transport error and carries no
		// information the caller does not have, while making the message twice
		// as long in a toast.
		var ue *url.Error
		if errors.As(err, &ue) {
			return ue.Err
		}
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &HTTPError{Status: res.StatusCode, Body: string(body), Path: path}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
