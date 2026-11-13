package embeddings

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Cosine is the cosine similarity of a and b, in [-1, 1]. It reports false
// when the vectors cannot be compared: different lengths (a vector from
// another model slipped through) or a zero vector.
func Cosine(a, b []float32) (float64, bool) {
	if len(a) != len(b) || len(a) == 0 {
		return 0, false
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0, false
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb)), true
}

// encode is a vector as the plaintext the encrypted store seals: little
// endian float32s, four bytes each.
func encode(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decode(b []byte) ([]float32, error) {
	if len(b)%4 != 0 || len(b) == 0 || len(b)/4 > maxDimensions {
		return nil, fmt.Errorf("embeddings: stored vector has %d bytes", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out, nil
}

// pgvectorText is a vector in pgvector's text form, "[1,2,3]". Sent as text
// and cast in SQL, so no driver type is needed for a column that exists
// only on installations that opted into pgvector.
func pgvectorText(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 10)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

func parsePgvector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("embeddings: not a pgvector literal")
	}
	parts := strings.Split(s[1:len(s)-1], ",")
	if len(parts) > maxDimensions {
		return nil, fmt.Errorf("embeddings: stored vector has %d dimensions", len(parts))
	}
	out := make([]float32, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("embeddings: stored vector: %w", err)
		}
		out[i] = float32(f)
	}
	return out, nil
}
