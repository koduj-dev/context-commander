package embeddings

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestCosine(t *testing.T) {
	cases := []struct {
		a, b []float32
		want float64
		ok   bool
	}{
		{[]float32{1, 0}, []float32{2, 0}, 1, true},
		{[]float32{1, 0}, []float32{0, 3}, 0, true},
		{[]float32{1, 1}, []float32{-1, -1}, -1, true},
		{[]float32{1, 0}, []float32{1, 0, 0}, 0, false},
		{[]float32{0, 0}, []float32{1, 0}, 0, false},
		{nil, nil, 0, false},
	}
	for _, c := range cases {
		got, ok := Cosine(c.a, c.b)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Cosine(%v, %v) = %v, %v; want %v, %v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func TestVectorEncodings(t *testing.T) {
	v := []float32{0.5, -1.25, 3e-7, 42}
	back, err := decode(encode(v))
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if back[i] != v[i] {
			t.Fatalf("binary round trip %v -> %v", v, back)
		}
	}
	if _, err := decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("a truncated vector decoded")
	}
	text := pgvectorText(v)
	parsed, err := parsePgvector(text)
	if err != nil {
		t.Fatal(err)
	}
	for i := range v {
		if parsed[i] != v[i] {
			t.Fatalf("pgvector round trip %s -> %v", text, parsed)
		}
	}
	if _, err := parsePgvector("1,2"); err == nil {
		t.Fatal("a literal without brackets parsed")
	}
}

// The cache's bound is bytes, and it evicts the least recently used vector
// first — a vector read since is kept over one that was not.
func TestVectorCacheIsBounded(t *testing.T) {
	vec := func() Vector { return Vector{MemoryID: uuid.New(), Model: "m", Values: make([]float32, 100)} }
	one := entryBytes(&Vector{Values: make([]float32, 100)})
	c := newVectorCache(3 * one)
	a, b, d := vec(), vec(), vec()
	c.put(a)
	c.put(b)
	c.put(d)
	if _, ok := c.get(a.MemoryID, "m"); !ok { // a is now the most recent
		t.Fatal("a missing before the cache was full")
	}
	c.put(vec())
	if used, limit := c.size(); used > limit {
		t.Fatalf("cache holds %d bytes over its %d limit", used, limit)
	}
	if _, ok := c.get(b.MemoryID, "m"); ok {
		t.Fatal("the least recently used vector was kept")
	}
	if _, ok := c.get(a.MemoryID, "m"); !ok {
		t.Fatal("a recently read vector was evicted")
	}
	if _, ok := c.get(a.MemoryID, "other-model"); ok {
		t.Fatal("a vector was served for another model")
	}
	c.forget(a.MemoryID)
	if _, ok := c.get(a.MemoryID, "m"); ok {
		t.Fatal("forget kept the vector")
	}
	c.put(Vector{MemoryID: uuid.New(), Values: make([]float32, 1000)}) // larger than the whole cache
	if used, limit := c.size(); used > limit {
		t.Fatalf("an oversized vector broke the bound: %d > %d", used, limit)
	}
}
