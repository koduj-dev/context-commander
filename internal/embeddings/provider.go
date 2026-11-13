// Package embeddings is semantic search for memories
// (docs/adr/0028-semantic-search.md): a Provider turns text into vectors, a
// Store keeps one vector per memory and model, and an Index ties them to
// internal/memories — it embeds memories in the background after they are
// written and ranks search candidates by meaning through the
// memories.Semantic seam.
//
// Nothing here is on unless an operator configures a provider. Without one,
// memories search by text exactly as before.
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Provider turns text into vectors. Implementations must be safe for
// concurrent use.
type Provider interface {
	// Name is the provider kind, "ollama" or "openai".
	Name() string
	// Model is the model every vector is made with. A vector is stored
	// under it, so changing it makes every stored vector stale.
	Model() string
	// Dimensions is the length of the vectors, or 0 until the first
	// successful Embed when it was not configured.
	Dimensions() int
	// Endpoint is where text is sent, for the startup log and the admin
	// status. It never carries a credential.
	Endpoint() string
	// Embed returns one vector per input, in input order.
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// Limits on what is sent and what is accepted. A provider is a network
// service this server does not control: a response is read through a limit
// and checked before anything trusts it.
const (
	// MaxInputRunes is how much of a memory is embedded. A memory body may be
	// 32 KiB; embedding models read a few thousand tokens and either refuse
	// more (the OpenAI API) or cut it silently (Ollama). Cutting here makes
	// both behave the same, and the title, which leads, is never lost.
	MaxInputRunes = 8000
	// MaxBatch is how many texts one request carries.
	MaxBatch = 16
	// maxResponseBytes bounds one response: 16 texts of 3072 dimensions in
	// JSON is about 1 MiB, so 16 MiB is far above any real answer.
	maxResponseBytes = 16 << 20
	// maxDimensions refuses a vector no real model produces, before it is
	// cached or stored.
	maxDimensions = 8192
	// DefaultTimeout bounds one request when none is configured.
	DefaultTimeout = 30 * time.Second
)

// Defaults per provider. bge-m3 is multilingual, reads Czech as well as
// English, and runs on a CPU, which is why it is the local default.
const (
	DefaultOllamaURL   = "http://localhost:11434"
	DefaultOllamaModel = "bge-m3"
	DefaultOpenAIURL   = "https://api.openai.com"
	DefaultOpenAIModel = "text-embedding-3-small"
)

// Config configures a provider.
type Config struct {
	// URL is the base URL: Ollama's http://host:11434, or the OpenAI-style
	// API root without /v1.
	URL   string
	Model string
	// APIKey is sent as a bearer token by the OpenAI-compatible provider.
	// Never logged, never part of an error.
	APIKey string
	// Dimensions, when set, is requested from an OpenAI-compatible API (the
	// text-embedding-3 models can shorten their vectors) and enforced on
	// every response from either provider.
	Dimensions int
	Timeout    time.Duration
	// Client replaces the HTTP client, for tests.
	Client *http.Client
}

// ErrProvider is any failure talking to the provider: unreachable, an error
// status, a malformed or oversized answer. The wrapped message says which,
// and never carries the text that was sent or the response body, which may
// echo it.
var ErrProvider = errors.New("embeddings: provider")

func providerErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrProvider, fmt.Sprintf(format, args...))
}

type httpProvider struct {
	name, model, endpoint string
	base                  *url.URL
	apiKey                string
	wantDims              int
	dims                  atomic.Int64
	client                *http.Client
}

func newHTTPProvider(name, defURL, defModel string, c Config) (*httpProvider, error) {
	raw := strings.TrimRight(strings.TrimSpace(c.URL), "/")
	if raw == "" {
		raw = defURL
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("embeddings: %s URL %q must be an absolute http or https URL", name, raw)
	}
	// A credential in the URL would end up in the startup log and the
	// admin status, which both print the endpoint.
	if u.User != nil {
		return nil, fmt.Errorf("embeddings: %s URL must not carry credentials; use the API key file", name)
	}
	if c.Dimensions < 0 || c.Dimensions > maxDimensions {
		return nil, fmt.Errorf("embeddings: dimensions must be between 1 and %d", maxDimensions)
	}
	p := &httpProvider{name: name, model: c.Model, base: u, endpoint: u.String(), apiKey: c.APIKey, wantDims: c.Dimensions}
	if p.model == "" {
		p.model = defModel
	}
	p.dims.Store(int64(c.Dimensions))
	p.client = c.Client
	if p.client == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}
		p.client = &http.Client{Timeout: timeout}
	}
	return p, nil
}

func (p *httpProvider) Name() string     { return p.name }
func (p *httpProvider) Model() string    { return p.model }
func (p *httpProvider) Endpoint() string { return p.endpoint }
func (p *httpProvider) Dimensions() int  { return int(p.dims.Load()) }

// post sends body to path and decodes a bounded response into out.
func (p *httpProvider) post(ctx context.Context, path string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("embeddings: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base.JoinPath(path).String(), bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		// net/http's error names the URL, which is configuration and safe;
		// it never includes the request body.
		return providerErr("%s request failed: %v", p.name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Read through the limit plus one byte, so an answer exactly at the
	// limit is accepted and one past it is refused rather than truncated
	// into something that might still parse.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return providerErr("%s response unreadable: %v", p.name, err)
	}
	if len(data) > maxResponseBytes {
		return providerErr("%s response larger than %d bytes", p.name, maxResponseBytes)
	}
	if resp.StatusCode != http.StatusOK {
		// The status only: an error body may quote the input back.
		return providerErr("%s answered HTTP %d", p.name, resp.StatusCode)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return providerErr("%s response is not the expected JSON", p.name)
	}
	return nil
}

// check validates vectors against the request and the dimensions seen so
// far, and learns the dimensions from the first good answer.
func (p *httpProvider) check(vectors [][]float32, n int) error {
	if len(vectors) != n {
		return providerErr("%s returned %d vectors for %d inputs", p.name, len(vectors), n)
	}
	want := int(p.dims.Load())
	for _, v := range vectors {
		if len(v) == 0 || len(v) > maxDimensions {
			return providerErr("%s returned a vector of %d dimensions", p.name, len(v))
		}
		if want == 0 {
			want = len(v)
		}
		if len(v) != want {
			return providerErr("%s returned %d dimensions, expected %d", p.name, len(v), want)
		}
		for _, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				return providerErr("%s returned a vector with a non-finite value", p.name)
			}
		}
	}
	p.dims.CompareAndSwap(0, int64(want))
	return nil
}

// Truncate cuts text to MaxInputRunes on a rune boundary.
func Truncate(s string) string {
	n := 0
	for i := range s {
		if n == MaxInputRunes {
			return s[:i]
		}
		n++
	}
	return s
}

func prepare(inputs []string) ([]string, error) {
	if len(inputs) == 0 || len(inputs) > MaxBatch {
		return nil, fmt.Errorf("embeddings: a request carries 1 to %d texts, not %d", MaxBatch, len(inputs))
	}
	out := make([]string, len(inputs))
	for i, s := range inputs {
		out[i] = Truncate(s)
	}
	return out, nil
}

// ---------------------------------------------------------------- Ollama

type ollama struct{ *httpProvider }

// NewOllama is a local Ollama server: POST {url}/api/embed.
func NewOllama(c Config) (Provider, error) {
	p, err := newHTTPProvider("ollama", DefaultOllamaURL, DefaultOllamaModel, c)
	if err != nil {
		return nil, err
	}
	return ollama{p}, nil
}

func (p ollama) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	in, err := prepare(inputs)
	if err != nil {
		return nil, err
	}
	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := p.post(ctx, "/api/embed", map[string]any{"model": p.model, "input": in}, &out); err != nil {
		return nil, err
	}
	if err := p.check(out.Embeddings, len(in)); err != nil {
		return nil, err
	}
	return out.Embeddings, nil
}

// ---------------------------------------------------------------- OpenAI-compatible

type openAI struct{ *httpProvider }

// NewOpenAI is an OpenAI-compatible embeddings API: POST {url}/v1/embeddings
// with a bearer key. Opt-in: it sends memory text to whoever runs that
// endpoint (docs/adr/0028).
func NewOpenAI(c Config) (Provider, error) {
	p, err := newHTTPProvider("openai", DefaultOpenAIURL, DefaultOpenAIModel, c)
	if err != nil {
		return nil, err
	}
	return openAI{p}, nil
}

func (p openAI) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	in, err := prepare(inputs)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"model": p.model, "input": in, "encoding_format": "float"}
	if p.wantDims > 0 {
		body["dimensions"] = p.wantDims
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := p.post(ctx, "/v1/embeddings", body, &out); err != nil {
		return nil, err
	}
	// The API returns an index per vector; order by it rather than trust
	// the array order, and refuse an answer whose indexes do not cover the
	// request exactly once.
	vectors := make([][]float32, len(in))
	if len(out.Data) != len(in) {
		return nil, providerErr("%s returned %d vectors for %d inputs", p.name, len(out.Data), len(in))
	}
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(in) || vectors[d.Index] != nil {
			return nil, providerErr("%s returned a bad or repeated index %d", p.name, d.Index)
		}
		vectors[d.Index] = d.Embedding
	}
	if err := p.check(vectors, len(in)); err != nil {
		return nil, err
	}
	return vectors, nil
}
