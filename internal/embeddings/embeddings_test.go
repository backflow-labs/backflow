package embeddings

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIEmbedder_PostsCorrectPayload(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotBody   map[string]any
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1]}]}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key", srv.URL, srv.Client())
	_, _ = e.Embed(context.Background(), "hello")

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/embeddings" {
		t.Errorf("path = %q, want /v1/embeddings", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want %q", gotAuth, "Bearer test-key")
	}
	if gotBody["model"] != "text-embedding-3-small" {
		t.Errorf("body.model = %v, want text-embedding-3-small", gotBody["model"])
	}
	if gotBody["input"] != "hello" {
		t.Errorf("body.input = %v, want hello", gotBody["input"])
	}
}

func TestOpenAIEmbedder_ParsesEmbedding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key", srv.URL, srv.Client())
	got, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	want := []float32{0.1, 0.2, 0.3}
	if len(got) != len(want) {
		t.Fatalf("len(embedding) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("embedding[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestOpenAIEmbedder_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key", srv.URL, srv.Client())
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed: want error for 429, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want it to mention 429", err.Error())
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, want it to include body snippet", err.Error())
	}
}

func TestOpenAIEmbedder_EmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	e := NewOpenAIEmbedder("test-key", srv.URL, srv.Client())
	_, err := e.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed: want error for empty data, got nil")
	}
}
