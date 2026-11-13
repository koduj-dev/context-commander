// Package embeddingstest is a fake embedding provider for tests: no network,
// deterministic, and "semantic" enough to tell a search by meaning from a
// search by words. Words are mapped to concepts, in English and Czech, so
// "databáze je pomalá" lands next to "the database is slow" while sharing
// none of its words, and unrelated text lands far from both.
package embeddingstest

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

// concepts maps a word to the dimension that stands for its meaning.
var concepts = map[string]int{
	"webhook": 0, "webhooks": 0, "callback": 0, "callbacks": 0, "zpětné": 0, "volání": 0, "notifikace": 0,
	"database": 1, "databáze": 1, "databázi": 1, "postgres": 1, "sql": 1, "query": 1, "dotaz": 1,
	"slow": 2, "pomalý": 2, "pomalá": 2, "pomalé": 2, "latency": 2, "performance": 2, "výkon": 2, "latence": 2,
	"retry": 3, "retries": 3, "opakování": 3, "znovu": 3, "backoff": 3,
	"deploy": 4, "deployment": 4, "nasazení": 4, "release": 4, "vydání": 4,
	"login": 5, "sign-in": 5, "přihlášení": 5, "password": 5, "heslo": 5,
}

// Dimensions is the length of every vector the fake returns.
const Dimensions = 16

// ErrDown is what Embed returns while the fake is set to fail.
var ErrDown = errors.New("embeddingstest: provider is down")

// Fake is a Provider. Safe for concurrent use.
type Fake struct {
	ModelName string
	calls     atomic.Int64
	mu        sync.Mutex
	down      bool
}

func (f *Fake) Name() string { return "fake" }
func (f *Fake) Model() string {
	if f.ModelName == "" {
		return "fake-model"
	}
	return f.ModelName
}
func (f *Fake) Dimensions() int  { return Dimensions }
func (f *Fake) Endpoint() string { return "http://fake.invalid" }

// SetDown makes Embed fail until it is set back.
func (f *Fake) SetDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

// Calls is how many times Embed was called.
func (f *Fake) Calls() int64 { return f.calls.Load() }

func (f *Fake) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	f.calls.Add(1)
	f.mu.Lock()
	down := f.down
	f.mu.Unlock()
	if down {
		return nil, ErrDown
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]float32, len(inputs))
	for i, s := range inputs {
		out[i] = Vector(s)
	}
	return out, nil
}

// Vector is the fake embedding of s: a concept word adds weight to its
// concept's dimension; any other word adds a little to one of the upper
// dimensions by hash, so unrelated texts are not identical.
func Vector(s string) []float32 {
	v := make([]float32, Dimensions)
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && r != '-' })
	for _, w := range words {
		if d, ok := concepts[w]; ok {
			v[d] += 1
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(w))
		v[8+int(h.Sum32()%8)] += 0.15
	}
	// Never the zero vector, which has no direction.
	v[Dimensions-1] += 0.01
	return v
}
