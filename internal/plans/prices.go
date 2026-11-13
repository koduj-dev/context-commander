package plans

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Prices is the operator's price table for estimating what reported tokens
// would have cost on a vendor's API (docs/adr/0022, "Time and tokens"). It
// is configuration, never built in: prices change, differ by contract and
// by region, and a number this server made up would be read as a fact.
//
// A nil *Prices is "no table configured" and estimates nothing.
type Prices struct {
	// Currency is an ISO 4217 code, so the interface can format the amount.
	Currency string
	// AsOf is when the operator took the prices, as they wrote it.
	AsOf   string
	exact  map[string]Price
	prefix []prefixPrice // longest prefix first
}

// Price is what one model charges per million tokens of each kind.
// CacheWrite is the five-minute cache write and CacheWrite1h the one-hour
// one; a table that does not give the latter prices one-hour writes at
// CacheWrite.
type Price struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheRead    float64 `json:"cache_read"`
	CacheWrite   float64 `json:"cache_write"`
	CacheWrite1h float64 `json:"cache_write_1h"`
}

type prefixPrice struct {
	prefix string
	price  Price
}

// Estimate is what an Effort's tokens would have cost at the table's prices.
// It is an estimate and only ever shown as one: the tokens are what clients
// reported, and the prices are what the operator configured.
type Estimate struct {
	Currency string
	AsOf     string
	// Amount covers only the models that have a price.
	Amount float64
	// Unpriced lists the models that had tokens but no price, "" being
	// tokens whose model was not stated. Empty means Amount covers every
	// reported token.
	Unpriced []string
}

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// LoadPrices reads a price table from a JSON file (deploy/usage-prices.example.json
// shows the format).
func LoadPrices(path string) (*Prices, error) {
	b, err := os.ReadFile(path) //nolint:gosec // operator-supplied --usage-prices, not network input
	if err != nil {
		return nil, fmt.Errorf("plans: usage prices: %w", err)
	}
	p, err := ParsePrices(b)
	if err != nil {
		return nil, fmt.Errorf("plans: usage prices %s: %w", path, err)
	}
	return p, nil
}

// ParsePrices parses a price table. Unknown fields are refused, so a typo
// such as "cache-read" fails at startup instead of silently pricing cache
// reads at zero. For the same reason every model must give all four rates:
// a missing one is an error, and a kind the vendor does not charge for is an
// explicit 0. The one-hour cache write, cache_write_1h, is the exception:
// it is optional and defaults to cache_write, so a table written before it
// existed keeps working and prices one-hour writes as it always did.
//
// A model key ending in "*" matches every model starting with what precedes
// it ("claude-opus-5*"); an exact key beats a prefix, and a longer prefix
// beats a shorter one.
func ParsePrices(b []byte) (*Prices, error) {
	var raw struct {
		Comment  string `json:"_comment"`
		Currency string `json:"currency"`
		AsOf     string `json:"as_of"`
		Models   map[string]struct {
			Input      *float64 `json:"input"`
			Output     *float64 `json:"output"`
			CacheRead  *float64 `json:"cache_read"`
			CacheWrite *float64 `json:"cache_write"`
			// Optional: absent prices one-hour writes at cache_write.
			CacheWrite1h *float64 `json:"cache_write_1h"`
		} `json:"models"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if !currencyCode.MatchString(raw.Currency) {
		return nil, fmt.Errorf("currency must be an ISO 4217 code such as USD, not %q", raw.Currency)
	}
	if len(raw.Models) == 0 {
		return nil, fmt.Errorf("no models priced")
	}
	p := &Prices{Currency: raw.Currency, AsOf: raw.AsOf, exact: map[string]Price{}}
	for key, rates := range raw.Models {
		if rates.Input == nil || rates.Output == nil || rates.CacheRead == nil || rates.CacheWrite == nil {
			return nil, fmt.Errorf("model %q: give all four rates (input, output, cache_read, cache_write); use 0 for a kind that is not charged", key)
		}
		price := Price{Input: *rates.Input, Output: *rates.Output, CacheRead: *rates.CacheRead, CacheWrite: *rates.CacheWrite, CacheWrite1h: *rates.CacheWrite}
		if rates.CacheWrite1h != nil {
			price.CacheWrite1h = *rates.CacheWrite1h
		}
		for _, v := range []float64{price.Input, price.Output, price.CacheRead, price.CacheWrite, price.CacheWrite1h} {
			if v < 0 {
				return nil, fmt.Errorf("model %q: a price cannot be negative", key)
			}
		}
		name, isPrefix := strings.CutSuffix(key, "*")
		if name == "" || !ValidModel(name) {
			return nil, fmt.Errorf("model %q is not a model id", key)
		}
		if isPrefix {
			p.prefix = append(p.prefix, prefixPrice{prefix: name, price: price})
		} else {
			p.exact[name] = price
		}
	}
	sort.Slice(p.prefix, func(i, j int) bool { return len(p.prefix[i].prefix) > len(p.prefix[j].prefix) })
	return p, nil
}

// Lookup is the price for a model, if the table has one. A model that was
// not stated has no price, whatever the table says.
func (p *Prices) Lookup(model string) (Price, bool) {
	if p == nil || model == "" {
		return Price{}, false
	}
	if price, ok := p.exact[model]; ok {
		return price, true
	}
	for _, pp := range p.prefix {
		if strings.HasPrefix(model, pp.prefix) {
			return pp.price, true
		}
	}
	return Price{}, false
}

// Cost is what t would have cost, if its model has a price. One-hour cache
// writes are part of CacheWriteTokens and priced at CacheWrite1h; the rest
// of the cache writes at CacheWrite.
func (p *Prices) Cost(t Tokens) (float64, bool) {
	price, ok := p.Lookup(t.Model)
	if !ok {
		return 0, false
	}
	const perMillion = 1_000_000
	hour := min(t.CacheWrite1hTokens, t.CacheWriteTokens)
	return (float64(t.TokensIn)*price.Input + float64(t.TokensOut)*price.Output +
		float64(t.CacheReadTokens)*price.CacheRead +
		float64(t.CacheWriteTokens-hour)*price.CacheWrite + float64(hour)*price.CacheWrite1h) / perMillion, true
}

// Estimate prices an Effort's per-model breakdown. Nil when there is no
// table or nothing to price: "no estimate" is not "costs nothing".
func (p *Prices) Estimate(e Effort) *Estimate {
	if p == nil || len(e.Models) == 0 {
		return nil
	}
	out := &Estimate{Currency: p.Currency, AsOf: p.AsOf}
	for _, m := range e.Models {
		if !m.any() {
			continue
		}
		if cost, ok := p.Cost(m); ok {
			out.Amount += cost
		} else {
			out.Unpriced = append(out.Unpriced, m.Model)
		}
	}
	return out
}

// SetPrices gives the service the operator's price table; nil (the default)
// estimates nothing.
func (s *Service) SetPrices(p *Prices) { s.prices = p }

// Prices is the configured table, nil when there is none. Adapters use it
// to put an estimate next to the tokens they show.
func (s *Service) Prices() *Prices { return s.prices }
