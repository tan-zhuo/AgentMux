package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentmux/internal/llm"
)

func TestEmbedFallsBackToTheOlderEndpoint(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if r.URL.Path == "/api/embed" {
			// What an Ollama predating the batch endpoint answers.
			http.NotFound(w, r)
			return
		}
		var req struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embedding": []float32{float32(len(req.Prompt)), 1, 2},
		})
	}))
	defer srv.Close()

	vecs, err := llm.New(srv.URL).Embed(context.Background(), "bge-m3", []string{"one", "three"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	// Order has to be preserved: the caller pairs vectors with rows by index.
	if vecs[0][0] != 3 || vecs[1][0] != 5 {
		t.Errorf("vectors came back in the wrong order: %v", vecs)
	}
	if len(seen) < 2 || seen[0] != "/api/embed" {
		t.Errorf("expected the batch endpoint to be tried first, saw %v", seen)
	}
}

func TestEmbedRejectsAShortBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings": [][]float32{{1, 2, 3}},
		})
	}))
	defer srv.Close()

	// Silently accepting two-for-one would pair every memory with the wrong
	// vector from that point on, and nothing downstream could detect it.
	_, err := llm.New(srv.URL).Embed(context.Background(), "bge-m3", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected an error when the server returns fewer vectors than inputs")
	}
}

func TestProbeReportsAMissingModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.14.2"})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "qwen3:8b", "size": 5_200_000_000}},
			})
		}
	}))
	defer srv.Close()

	st := llm.New(srv.URL).Probe(context.Background(), "qwen3:8b", "bge-m3")
	if !st.Reachable || st.Version != "0.14.2" {
		t.Fatalf("probe should have reached the server: %+v", st)
	}
	if !st.ChatModelReady {
		t.Error("qwen3:8b is installed and should be reported ready")
	}
	if st.EmbedModelReady {
		t.Error("bge-m3 is not installed and should not be reported ready")
	}
	if !strings.Contains(st.Hint, "ollama pull bge-m3") {
		t.Errorf("the hint should name the command to run, got %q", st.Hint)
	}
}

// TestProbeMatchesTheImplicitLatestTag covers configuring "bge-m3" against an
// installed "bge-m3:latest", which is the same model and used to read as missing.
func TestProbeMatchesTheImplicitLatestTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.14.2"})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{{"name": "bge-m3:latest"}},
			})
		}
	}))
	defer srv.Close()

	st := llm.New(srv.URL).Probe(context.Background(), "", "bge-m3")
	if !st.EmbedModelReady {
		t.Error("bge-m3:latest should satisfy a configured bge-m3")
	}
}

func TestProbeExplainsAnUnreachableRuntime(t *testing.T) {
	// A port nothing listens on: the normal state of a machine where Ollama has
	// not been started.
	//
	// This asserts only that the answer is useful, not which branch produced
	// it. How the kernel words a failed connection differs between machines —
	// a CI runner and a laptop disagreed here once — so the branches are pinned
	// against synthetic errors in diagnose_test.go instead.
	st := llm.New("http://127.0.0.1:1").Probe(context.Background(), "qwen3:8b", "bge-m3")
	if st.Reachable {
		t.Fatal("nothing is listening there")
	}
	if !strings.Contains(st.Hint, "ollama serve") {
		t.Errorf("the hint should say how to start it, got %q", st.Hint)
	}
}
