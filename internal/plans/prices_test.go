package plans_test

import (
	"math"
	"path/filepath"
	"slices"
	"testing"

	"github.com/koduj-dev/context-commander/internal/plans"
)

// The example the operator copies has to load: a broken example is the
// first thing someone configuring estimates would hit.
func TestTheExamplePriceTableLoads(t *testing.T) {
	p, err := plans.LoadPrices(filepath.Join("..", "..", "deploy", "usage-prices.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Currency != "USD" || p.AsOf == "" {
		t.Errorf("currency %q, as of %q", p.Currency, p.AsOf)
	}
	if _, ok := p.Lookup("claude-opus-5-5"); !ok {
		t.Error("the example prices no Claude model")
	}
}

func TestAnEstimatePricesOnlyWhatHasAPrice(t *testing.T) {
	p, err := plans.ParsePrices([]byte(`{
		"currency": "EUR", "as_of": "2026-11-03",
		"models": {
			"opus-x":  {"input": 4, "output": 20, "cache_read": 0.2, "cache_write": 5},
			"haiku*":  {"input": 1, "output": 1, "cache_read": 1, "cache_write": 1},
			"haiku-9*": {"input": 2, "output": 2, "cache_read": 2, "cache_write": 2}
		}}`))
	if err != nil {
		t.Fatal(err)
	}
	opus := plans.Tokens{Model: "opus-x", TokensIn: 1_000_000, TokensOut: 1_000_000, CacheReadTokens: 10_000_000, CacheWriteTokens: 1_000_000}
	if cost, ok := p.Cost(opus); !ok || math.Abs(cost-(4+20+2+5)) > 1e-9 {
		t.Errorf("opus-x cost %v %v, want 31", cost, ok)
	}
	// The longest prefix wins, and an exact key needs an exact model.
	if cost, _ := p.Cost(plans.Tokens{Model: "haiku-9-1", TokensIn: 1_000_000}); cost != 2 {
		t.Errorf("haiku-9-1 cost %v, want the longer prefix's 2", cost)
	}
	if _, ok := p.Lookup("opus-x-2"); ok {
		t.Error("an exact key matched a longer model id")
	}
	// A model that was not stated has no price, even under a catch-all.
	if _, ok := p.Lookup(""); ok {
		t.Error("an unstated model was priced")
	}

	e := plans.Effort{Models: []plans.Tokens{{Model: "", TokensIn: 5}, {Model: "gpt-x", TokensOut: 9}, opus}}
	est := p.Estimate(e)
	if est == nil || est.Currency != "EUR" || math.Abs(est.Amount-31) > 1e-9 || !slices.Equal(est.Unpriced, []string{"", "gpt-x"}) {
		t.Errorf("estimate %+v", est)
	}
	if p.Estimate(plans.Effort{}) != nil {
		t.Error("an estimate of nothing is not zero, it is no estimate")
	}
	var none *plans.Prices
	if none.Estimate(e) != nil {
		t.Error("no table, yet an estimate")
	}
}

func TestABrokenPriceTableIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"typo in a field":  `{"currency":"USD","models":{"m":{"input":1,"cache-read":1}}}`,
		"no currency":      `{"models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"not a currency":   `{"currency":"dollars","models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"nothing priced":   `{"currency":"USD","models":{}}`,
		"negative price":   `{"currency":"USD","models":{"m":{"input":-1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"not a model id":   `{"currency":"USD","models":{"a model":{"input":1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"a bare wildcard":  `{"currency":"USD","models":{"*":{"input":1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"not even JSON":    `currency: USD`,
		"a missing rate":   `{"currency":"USD","models":{"m":{"input":1,"output":1,"cache_read":1}}}`,
		"a null rate":      `{"currency":"USD","models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write":null}}}`,
		"an unknown field": `{"currency":"USD","per":1000,"models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write":1}}}`,
		"a negative 1h":    `{"currency":"USD","models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write":1,"cache_write_1h":-2}}}`,
		"only the 1h rate": `{"currency":"USD","models":{"m":{"input":1,"output":1,"cache_read":1,"cache_write_1h":2}}}`,
	} {
		if _, err := plans.ParsePrices([]byte(body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// A kind the vendor does not charge for is an explicit zero, and fine.
	if _, err := plans.ParsePrices([]byte(`{"currency":"USD","models":{"m":{"input":1,"output":1,"cache_read":0.1,"cache_write":0}}}`)); err != nil {
		t.Errorf("an explicit zero was refused: %v", err)
	}
}

// cache_write_1h is optional and falls back to cache_write, so a table
// written before it existed prices one-hour writes as it always did; given,
// it prices the one-hour part of the cache writes and only that part.
func TestOneHourCacheWritesHaveTheirOwnOptionalRate(t *testing.T) {
	old, err := plans.ParsePrices([]byte(`{"currency":"USD","models":{"m":{"input":0,"output":0,"cache_read":0,"cache_write":5}}}`))
	if err != nil {
		t.Fatal(err)
	}
	withRate, err := plans.ParsePrices([]byte(`{"currency":"USD","models":{"m":{"input":0,"output":0,"cache_read":0,"cache_write":5,"cache_write_1h":8}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := old.Lookup("m"); p.CacheWrite1h != 5 {
		t.Errorf("without cache_write_1h the one-hour rate is %v, want cache_write's 5", p.CacheWrite1h)
	}
	tokens := plans.Tokens{Model: "m", CacheWriteTokens: 1_000_000, CacheWrite1hTokens: 250_000}
	for _, c := range []struct {
		name   string
		prices *plans.Prices
		want   float64
	}{
		{"without a one-hour rate", old, 5},
		{"with one", withRate, 0.75*5 + 0.25*8},
	} {
		if got, ok := c.prices.Cost(tokens); !ok || got != c.want {
			t.Errorf("%s: cost %v, want %v", c.name, got, c.want)
		}
	}
}
