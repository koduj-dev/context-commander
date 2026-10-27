// Package quota names the one outcome of a storage quota a caller sees: the
// write was refused because its namespace is full. The counting and the
// refusing both happen in the database (migrations/000030,
// docs/adr/0021-storage-quotas-per-namespace.md), so there is no Go-side
// counter here to keep in step with it.
package quota

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrExceeded is a write refused because it would take its namespace over
// its storage limit. Deliberately carries no numbers: it reaches whoever was
// writing, who may be a project member outside the organization that owns
// the namespace.
var ErrExceeded = errors.New("storage quota exceeded: this namespace has no room for the write — delete artifacts or ask an installation administrator for a larger limit")

// exceededSQLState is what cc_storage_quota_exceeded() raises.
const exceededSQLState = "CCQ01"

// Translate turns the database's refusal into ErrExceeded and returns every
// other error unchanged, nil included. Call it on the error of a
// transaction that inserts something the quota triggers count.
func Translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == exceededSQLState {
		return ErrExceeded
	}
	return err
}

// ParseSize reads a byte count the way an operator writes one on a command
// line: a plain number of bytes, or a number with a decimal (KB, MB, GB, TB)
// or binary (KiB, MiB, GiB, TiB) suffix, case-insensitive. Fractions are
// allowed with a suffix ("1.5GB"). An empty string is 0, which means no limit.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	units := []struct {
		suffix string
		factor float64
	}{
		// Longest first, so "KiB" is not read as "B" after a number "1Ki".
		{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30}, {"tib", 1 << 40},
		{"kb", 1e3}, {"mb", 1e6}, {"gb", 1e9}, {"tb", 1e12},
		{"b", 1},
	}
	lower := strings.ToLower(s)
	for _, u := range units {
		if !strings.HasSuffix(lower, u.suffix) {
			continue
		}
		num := strings.TrimSpace(lower[:len(lower)-len(u.suffix)])
		v, err := strconv.ParseFloat(num, 64)
		if err != nil || v < 0 {
			return 0, fmt.Errorf("quota: %q is not a size (examples: 500MB, 10GB, 2GiB)", s)
		}
		bytes := v * u.factor
		if bytes > float64(1<<62) {
			return 0, fmt.Errorf("quota: %q is too large", s)
		}
		return int64(bytes), nil
	}
	v, err := strconv.ParseInt(lower, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("quota: %q is not a size (examples: 500MB, 10GB, 2GiB)", s)
	}
	return v, nil
}
