package plans_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/koduj-dev/context-commander/internal/plans"
	"github.com/koduj-dev/context-commander/internal/projects"
)

func (f *fixture) usage(m member, group string) plans.UsageSummary {
	f.t.Helper()
	q, err := plans.ParseUsageQuery("", "", group, f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	q.ProjectID = &f.projectID
	s, err := f.svc.Usage(f.ctx, m.principalID, q)
	if err != nil {
		f.t.Fatalf("Usage(%q): %v", group, err)
	}
	return s
}

// The ledger counts time present and tokens per branch and ticket, step or
// no step, and a summary folds models away unless asked for them.
func TestLedgerCountsTimeAndTokensByBranchAndTicket(t *testing.T) {
	f := newFixture(t)
	prices, err := plans.ParsePrices([]byte(`{"currency":"USD","as_of":"2026-11","models":{"claude-opus-5-5":{"input":10,"output":50,"cache_read":1,"cache_write":12}}}`))
	if err != nil {
		t.Fatal(err)
	}
	f.svc.SetPrices(prices)
	agent := f.join(f.projectID, "Claude", projects.RoleContributor)

	// No step at all: present on a branch and ticket.
	if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID,
		plans.Report{Activity: "looking around", Branch: "feature/export", TicketKey: "PROJ-7"}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(5 * time.Minute)
	opus := plans.Tokens{Model: "claude-opus-5-5", TokensIn: 1_000_000, TokensOut: 10}
	if _, ok, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, opus, plans.UsageContext{}); err != nil || !ok {
		t.Fatalf("usage: ok=%v err=%v", ok, err)
	}
	// A report without a branch keeps the one given before.
	f.clock = f.clock.Add(2 * time.Minute)
	if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID, plans.Report{Activity: "still here"}); err != nil {
		t.Fatal(err)
	}
	// The hook names another branch: the tokens and the time after belong to it.
	f.clock = f.clock.Add(1 * time.Minute)
	other := plans.Tokens{TokensOut: 7}
	if _, _, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, other, plans.UsageContext{Branch: "main"}); err != nil {
		t.Fatal(err)
	}

	s := f.usage(agent, "branch,ticket")
	if s.Total.Seconds != 8*60 {
		t.Errorf("total seconds = %d, want 480", s.Total.Seconds)
	}
	if s.Total.Tokens.TokensIn != 1_000_000 || s.Total.Tokens.TokensOut != 17 || s.Total.Turns != 2 {
		t.Errorf("total tokens %+v turns %d", s.Total.Tokens, s.Total.Turns)
	}
	byBranch := map[string]plans.UsageRow{}
	for _, r := range s.Rows {
		byBranch[*r.Branch+"|"+*r.TicketKey] = r
	}
	feature := byBranch["feature/export|PROJ-7"]
	if feature.Seconds != 7*60 || feature.Tokens.TokensIn != 1_000_000 {
		t.Errorf("feature/export row = %+v, want 420 s and the opus tokens", feature)
	}
	main := byBranch["main|PROJ-7"]
	if main.Seconds != 60 || main.Tokens.TokensOut != 7 {
		t.Errorf("main row = %+v, want 60 s and 7 tokens out (rows %+v)", main, s.Rows)
	}
	// Priced: the opus tokens cost 10 + 0.0005; the unnamed model has no price.
	if feature.Cost == nil || *feature.Cost < 10 || *feature.Cost > 10.001 {
		t.Errorf("feature cost = %v", feature.Cost)
	}
	if len(s.Unpriced) != 1 || s.Unpriced[0] != "" {
		t.Errorf("unpriced = %v, want the unstated model", s.Unpriced)
	}

	byModel := f.usage(agent, "model,principal")
	models := map[string]bool{}
	for _, r := range byModel.Rows {
		models[*r.Model] = true
		if r.PrincipalName != "Claude" {
			t.Errorf("principal name = %q, want Claude", r.PrincipalName)
		}
	}
	if !models["claude-opus-5-5"] || !models[""] {
		t.Errorf("models = %v", models)
	}
}

// A hook configured with a project counts a client's first turn even before
// it ever reported, instead of dropping it.
func TestLedgerHookWithAProjectPlacesTheClient(t *testing.T) {
	f := newFixture(t)
	agent := f.join(f.projectID, "Codex", projects.RoleContributor)
	if _, ok, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID,
		plans.Tokens{Model: "gpt-x", TokensOut: 42}, plans.UsageContext{Branch: "fix/login", ProjectID: &f.projectID}); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	s := f.usage(agent, "branch")
	if len(s.Rows) != 1 || *s.Rows[0].Branch != "fix/login" || s.Rows[0].Tokens.TokensOut != 42 {
		t.Fatalf("rows = %+v", s.Rows)
	}
	b, _ := f.svc.Board(f.ctx, agent.principalID, f.projectID)
	if len(b.Presence) != 1 {
		t.Fatalf("presence = %+v, want the client placed", b.Presence)
	}
}

func TestLedgerIsProjectScopedAndValidated(t *testing.T) {
	f := newFixture(t)
	agent := f.join(f.projectID, "Claude", projects.RoleContributor)
	if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID,
		plans.Report{Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Minute)
	if _, _, err := f.svc.ReportUsage(f.ctx, agent.principalID, &agent.clientID, plans.Tokens{TokensIn: 1}, plans.UsageContext{}); err != nil {
		t.Fatal(err)
	}

	// Someone in another project sees nothing of this one, across projects
	// or by naming it.
	otherProject, _ := f.newProject("other")
	stranger := f.join(otherProject, "Stranger", projects.RoleOwner)
	q, _ := plans.ParseUsageQuery("", "", "project", f.clock)
	s, err := f.svc.Usage(f.ctx, stranger.principalID, q)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total.Tokens.TokensIn != 0 || len(s.Rows) != 0 {
		t.Fatalf("a stranger's cross-project summary saw %+v", s)
	}
	q.ProjectID = &f.projectID
	if _, err := f.svc.Usage(f.ctx, stranger.principalID, q); err == nil {
		t.Fatal("a stranger summed a project they are not in")
	}

	for _, bad := range []plans.Report{
		{Branch: strings.Repeat("b", plans.MaxBranch+1)},
		{TicketKey: "PROJ\x01-1"},
	} {
		if _, err := f.svc.Report(f.ctx, agent.principalID, &agent.clientID, f.projectID, bad); !errors.Is(err, plans.ErrInvalid) {
			t.Errorf("Report(%+v) = %v, want ErrInvalid", bad, err)
		}
	}
	if _, err := plans.ParseUsageQuery("2026-01-01", "2027-06-01", "", f.clock); err != nil {
		t.Fatal(err) // parsing accepts it; the range check is Usage's
	}
	long, _ := plans.ParseUsageQuery("2025-01-01", "2026-06-01", "", f.clock)
	if _, err := f.svc.Usage(f.ctx, agent.principalID, long); !errors.Is(err, plans.ErrInvalid) {
		t.Errorf("a range over %d days: %v, want ErrInvalid", plans.MaxUsageDays, err)
	}
	if _, err := plans.ParseUsageQuery("", "", "day,secret_column", f.clock); !errors.Is(err, plans.ErrInvalid) {
		t.Errorf("an unknown group: %v, want ErrInvalid", err)
	}
}
