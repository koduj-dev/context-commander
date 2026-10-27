package quota

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseSize(t *testing.T) {
	cases := map[string]int64{
		"":        0,
		"0":       0,
		"1024":    1024,
		"500MB":   500_000_000,
		"10gb":    10_000_000_000,
		"2GiB":    2 << 30,
		"1.5 GB":  1_500_000_000,
		"64KiB":   64 << 10,
		"7b":      7,
		" 1TB ":   1_000_000_000_000,
		"0.5 mib": 1 << 19,
	}
	for in, want := range cases {
		got, err := ParseSize(in)
		if err != nil {
			t.Errorf("ParseSize(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseSize(%q) = %d, want %d", in, got, want)
		}
	}
	for _, bad := range []string{"ten", "-1", "-5GB", "1.5", "GB", "5 PB", "99999999999TB"} {
		if _, err := ParseSize(bad); err == nil {
			t.Errorf("ParseSize(%q) accepted", bad)
		}
	}
}

func TestTranslate(t *testing.T) {
	refusal := fmt.Errorf("insert: %w", &pgconn.PgError{Code: "CCQ01", Message: "storage quota exceeded"})
	if !errors.Is(Translate(refusal), ErrExceeded) {
		t.Error("the database's refusal was not translated")
	}
	other := &pgconn.PgError{Code: "23505"}
	if got := Translate(other); !errors.Is(got, other) {
		t.Errorf("an unrelated error came back as %v", got)
	}
	if Translate(nil) != nil {
		t.Error("nil did not stay nil")
	}
}
