package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fakeServer answers one path with handler and records the last request.
type fakeServer struct {
	*httptest.Server
	lastBody []byte
	lastAuth string
	lastPath string
}

func newFakeServer(t *testing.T, handler func(w http.ResponseWriter, body map[string]any)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastBody, _ = io.ReadAll(r.Body)
		f.lastAuth, f.lastPath = r.Header.Get("Authorization"), r.URL.Path
		var body map[string]any
		_ = json.Unmarshal(f.lastBody, &body)
		handler(w, body)
	}))
	t.Cleanup(f.Close)
	return f
}

func inputsOf(body map[string]any) []any { in, _ := body["input"].([]any); return in }

func TestOllamaEmbeds(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, body map[string]any) {
		out := [][]float32{}
		for range inputsOf(body) {
			out = append(out, []float32{0.1, 0.2, 0.3})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	})
	p, err := NewOllama(Config{URL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[1]) != 3 || p.Dimensions() != 3 || p.Model() != DefaultOllamaModel {
		t.Fatalf("got %v dims %d model %s", got, p.Dimensions(), p.Model())
	}
	if srv.lastPath != "/api/embed" || srv.lastAuth != "" || !strings.Contains(string(srv.lastBody), `"model":"bge-m3"`) {
		t.Fatalf("request %s %q %s", srv.lastPath, srv.lastAuth, srv.lastBody)
	}
}

func TestOpenAIEmbedsInIndexOrderWithTheKey(t *testing.T) {
	srv := newFakeServer(t, func(w http.ResponseWriter, body map[string]any) {
		// Returned out of order: the provider must sort by index.
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"index": 1, "embedding": []float32{0, 1}},
			{"index": 0, "embedding": []float32{1, 0}},
		}})
	})
	p, err := NewOpenAI(Config{URL: srv.URL, APIKey: "sk-test-secret", Model: "m", Dimensions: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0][0] != 1 || got[1][1] != 1 {
		t.Fatalf("not in index order: %v", got)
	}
	if srv.lastPath != "/v1/embeddings" || srv.lastAuth != "Bearer sk-test-secret" || !strings.Contains(string(srv.lastBody), `"dimensions":2`) {
		t.Fatalf("request %s %q %s", srv.lastPath, srv.lastAuth, srv.lastBody)
	}
}

func TestProviderRefusesBadAnswers(t *testing.T) {
	cases := map[string]struct {
		openai bool
		write  func(w http.ResponseWriter)
		want   string
	}{
		"error status, body not quoted": {write: func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"input was: TOP-SECRET-MEMORY"}`)
		}, want: "HTTP 500"},
		"not json":          {write: func(w http.ResponseWriter) { _, _ = io.WriteString(w, "<html>proxy</html>") }, want: "not the expected JSON"},
		"too few vectors":   {write: func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"embeddings":[[1,2]]}`) }, want: "1 vectors for 2 inputs"},
		"ragged dimensions": {write: func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"embeddings":[[1,2],[1,2,3]]}`) }, want: "expected 2"},
		"empty vector":      {write: func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"embeddings":[[],[]]}`) }, want: "0 dimensions"},
		"oversized": {write: func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"embeddings":[[`+strings.Repeat("1,", maxResponseBytes/2)+`1]]}`)
		}, want: "larger than"},
		"openai repeated index": {openai: true, write: func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[1]}]}`)
		}, want: "repeated index"},
		"openai index out of range": {openai: true, write: func(w http.ResponseWriter) {
			_, _ = io.WriteString(w, `{"data":[{"index":0,"embedding":[1]},{"index":7,"embedding":[1]}]}`)
		}, want: "bad or repeated index"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			srv := newFakeServer(t, func(w http.ResponseWriter, _ map[string]any) { c.write(w) })
			var p Provider
			var err error
			if c.openai {
				p, err = NewOpenAI(Config{URL: srv.URL, APIKey: "sk-test-secret"})
			} else {
				p, err = NewOllama(Config{URL: srv.URL})
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Embed(context.Background(), []string{"TOP-SECRET-MEMORY", "two"})
			if !errors.Is(err, ErrProvider) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrProvider containing %q", err, c.want)
			}
			// Neither the text sent, nor a response quoting it, nor the
			// key may end up in an error, which is logged and shown to
			// administrators.
			if strings.Contains(err.Error(), "TOP-SECRET") || strings.Contains(err.Error(), "sk-test") {
				t.Fatalf("error leaks content or the key: %v", err)
			}
		})
	}
}

func TestProviderTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	p, err := NewOllama(Config{URL: srv.URL, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = p.Embed(context.Background(), []string{"x"})
	if !errors.Is(err, ErrProvider) {
		t.Fatalf("err = %v, want a provider timeout", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the timeout did not bound the request: %s", time.Since(start))
	}
}

func TestProviderConfigIsChecked(t *testing.T) {
	for _, u := range []string{"ftp://host", "not a url", "http://user:pass@host:11434"} {
		if _, err := NewOllama(Config{URL: u}); err == nil {
			t.Errorf("URL %q accepted", u)
		}
	}
	if _, err := NewOpenAI(Config{Dimensions: maxDimensions + 1}); err == nil {
		t.Error("absurd dimensions accepted")
	}
	p, _ := NewOllama(Config{})
	if _, err := p.Embed(context.Background(), make([]string, MaxBatch+1)); err == nil {
		t.Error("an oversized batch was sent")
	}
}

func TestTruncateCutsOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("ž", MaxInputRunes+10)
	got := Truncate(long)
	if utf8.RuneCountInString(got) != MaxInputRunes || !utf8.ValidString(got) {
		t.Fatalf("truncated to %d runes, valid %v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if Truncate("short") != "short" {
		t.Fatal("short text changed")
	}
}
