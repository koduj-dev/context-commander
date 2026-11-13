package plans

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/koduj-dev/context-commander/internal/auth"
	"github.com/koduj-dev/context-commander/internal/db"
	"github.com/koduj-dev/context-commander/internal/projects"
)

// The usage ledger (docs/adr/0027, migrations/000039): time present and
// tokens reported, added up per project, UTC day, principal, model, branch
// and ticket.

const (
	// MaxBranch bounds a branch name. Git allows longer; nobody types one.
	MaxBranch = 200
	// MaxTicketKey bounds a ticket key such as PROJ-1234.
	MaxTicketKey = 100
	// MaxUsageDays bounds one summary's range.
	MaxUsageDays = 366
	// MaxUsageRows bounds one summary's grouped rows; the answer says when
	// the bound cut it.
	MaxUsageRows = 1000
)

// checkWorkContext trims a branch and a ticket key and refuses what cannot
// be one: too long, or carrying control characters that would end up in a
// table cell and a log line.
func checkWorkContext(branch, ticketKey string) (string, string, error) {
	branch, ticketKey = strings.TrimSpace(branch), strings.TrimSpace(ticketKey)
	if len(branch) > MaxBranch {
		return "", "", invalid("branch is at most %d characters", MaxBranch)
	}
	if len(ticketKey) > MaxTicketKey {
		return "", "", invalid("ticket_key is at most %d characters", MaxTicketKey)
	}
	for _, v := range []string{branch, ticketKey} {
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return "", "", invalid("branch and ticket_key cannot contain control characters")
		}
	}
	return branch, ticketKey, nil
}

type ledgerKey struct {
	project   uuid.UUID
	branch    string
	ticketKey string
}

// creditLedger adds seconds and tokens to the day's rows. Seconds go to the
// model = '' row, because time is not spent per model; tokens to their
// model's row, which is the same row when the model was not stated.
func creditLedger(ctx context.Context, tx pgx.Tx, key ledgerKey, principalID uuid.UUID, now time.Time, seconds int64, tokens Tokens) error {
	if key.project == uuid.Nil {
		return nil
	}
	day := now.UTC().Format(time.DateOnly)
	upsert := func(model string, seconds int64, t Tokens, turns int64) error {
		_, err := tx.Exec(ctx,
			`INSERT INTO usage_ledger (project_id, day, principal_id, model, branch, ticket_key,
			                           seconds, tokens_in, tokens_out, cache_read_tokens, cache_write_tokens, turns)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (project_id, day, principal_id, model, branch, ticket_key) DO UPDATE SET
			   seconds = usage_ledger.seconds + EXCLUDED.seconds,
			   tokens_in = usage_ledger.tokens_in + EXCLUDED.tokens_in,
			   tokens_out = usage_ledger.tokens_out + EXCLUDED.tokens_out,
			   cache_read_tokens = usage_ledger.cache_read_tokens + EXCLUDED.cache_read_tokens,
			   cache_write_tokens = usage_ledger.cache_write_tokens + EXCLUDED.cache_write_tokens,
			   turns = usage_ledger.turns + EXCLUDED.turns`,
			key.project, day, principalID, model, key.branch, key.ticketKey,
			seconds, t.TokensIn, t.TokensOut, t.CacheReadTokens, t.CacheWriteTokens, turns)
		return err
	}
	switch {
	case tokens.any() && tokens.Model == "":
		return upsert("", seconds, tokens, 1)
	case tokens.any():
		if seconds > 0 {
			if err := upsert("", seconds, Tokens{}, 0); err != nil {
				return err
			}
		}
		return upsert(tokens.Model, 0, tokens, 1)
	case seconds > 0:
		return upsert("", seconds, Tokens{}, 0)
	}
	return nil
}

// Usage dimensions a summary can be grouped by.
const (
	GroupProject   = "project"
	GroupDay       = "day"
	GroupPrincipal = "principal"
	GroupModel     = "model"
	GroupBranch    = "branch"
	GroupTicket    = "ticket"
)

// groupColumns maps a dimension to its column. An allowlist: the grouping
// is spliced into SQL, so nothing a caller sends is.
var groupColumns = map[string]string{
	GroupProject:   "l.project_id",
	GroupDay:       "l.day",
	GroupPrincipal: "l.principal_id",
	GroupModel:     "l.model",
	GroupBranch:    "l.branch",
	GroupTicket:    "l.ticket_key",
}

// groupOrder is the fixed order dimensions are selected and sorted in.
var groupOrder = []string{GroupProject, GroupDay, GroupPrincipal, GroupModel, GroupBranch, GroupTicket}

// UsageQuery asks for a summary.
type UsageQuery struct {
	// ProjectID limits the summary to one project. Nil covers every project
	// the caller can read plans in.
	ProjectID *uuid.UUID
	// From and To are UTC days, both inclusive.
	From, To time.Time
	// GroupBy names the dimensions to group by; empty gives one total.
	GroupBy []string
}

// UsageRow is one group's sums. Only the dimensions grouped by are set.
type UsageRow struct {
	ProjectID     *uuid.UUID
	ProjectPath   string
	Day           *time.Time
	PrincipalID   *uuid.UUID
	PrincipalKind string
	PrincipalName string
	Model         *string
	Branch        *string
	TicketKey     *string

	Seconds int64
	Tokens  Tokens
	Turns   int64
	// Cost is the estimate at the operator's prices, nil without a table.
	Cost *float64
}

// UsageSummary is the grouped rows and their total.
type UsageSummary struct {
	Rows  []UsageRow
	Total UsageRow
	// Currency and AsOf describe the price table; empty without one.
	Currency string
	AsOf     string
	// Unpriced lists models that had tokens and no price.
	Unpriced []string
	// Truncated means MaxUsageRows cut the rows. The total is still whole.
	Truncated bool
}

// ParseGroupBy reads a comma-separated list of dimensions.
func ParseGroupBy(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, g := range strings.Split(raw, ",") {
		g = strings.TrimSpace(g)
		if g == "" || seen[g] {
			continue
		}
		if _, ok := groupColumns[g]; !ok {
			return nil, invalid("group is a comma-separated list of: %s", strings.Join(groupOrder, ", "))
		}
		seen[g] = true
		out = append(out, g)
	}
	return out, nil
}

// DefaultUsageDays is the range a summary covers when none is given.
const DefaultUsageDays = 30

// ParseUsageQuery reads the query a caller sends as text: from and to as
// YYYY-MM-DD UTC days (default: the DefaultUsageDays days up to today), and a
// comma-separated grouping.
func ParseUsageQuery(from, to, group string, now time.Time) (UsageQuery, error) {
	var q UsageQuery
	var err error
	q.To = now.UTC().Truncate(24 * time.Hour)
	if to != "" {
		if q.To, err = time.Parse(time.DateOnly, to); err != nil {
			return UsageQuery{}, invalid("to is a day, YYYY-MM-DD")
		}
	}
	q.From = q.To.AddDate(0, 0, -(DefaultUsageDays - 1))
	if from != "" {
		if q.From, err = time.Parse(time.DateOnly, from); err != nil {
			return UsageQuery{}, invalid("from is a day, YYYY-MM-DD")
		}
	}
	if q.GroupBy, err = ParseGroupBy(group); err != nil {
		return UsageQuery{}, err
	}
	return q, nil
}

// Usage sums the ledger. Rows are always read grouped by model as well, so
// each can be priced; the model is folded away afterwards when it was not
// asked for.
func (s *Service) Usage(ctx context.Context, principalID uuid.UUID, q UsageQuery) (UsageSummary, error) {
	from, to := q.From.UTC().Truncate(24*time.Hour), q.To.UTC().Truncate(24*time.Hour)
	if to.Before(from) {
		return UsageSummary{}, invalid("to is before from")
	}
	if to.Sub(from) > MaxUsageDays*24*time.Hour {
		return UsageSummary{}, invalid("a summary covers at most %d days", MaxUsageDays)
	}
	asked := map[string]bool{}
	for _, g := range q.GroupBy {
		if _, ok := groupColumns[g]; !ok {
			return UsageSummary{}, invalid("unknown group %q", g)
		}
		asked[g] = true
	}
	if q.ProjectID != nil {
		if err := s.checkCapability(ctx, principalID, *q.ProjectID, auth.CapPlanRead); err != nil {
			return UsageSummary{}, err
		}
	}

	var dims []string
	for _, g := range groupOrder {
		if asked[g] || g == GroupModel {
			dims = append(dims, g)
		}
	}
	cols := make([]string, len(dims))
	for i, g := range dims {
		cols[i] = groupColumns[g]
	}
	selectCols := strings.Join(cols, ", ")

	var raw []UsageRow
	paths := map[uuid.UUID]string{}
	err := db.WithPrincipal(ctx, s.pool, db.Principal{ID: principalID}, func(tx pgx.Tx) error {
		// The projects to read: one, or every membership whose role may read
		// plans. RLS would scope the rows anyway; the role check is what
		// keeps a role without plan.read out, as in SearchAll.
		memberships, err := projects.NewStore().ListForPrincipal(ctx, tx, principalID)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for _, ms := range memberships {
			if q.ProjectID != nil && ms.Project.ID != *q.ProjectID {
				continue
			}
			if !auth.RoleCan(ms.Role, auth.CapPlanRead) {
				continue
			}
			ids = append(ids, ms.Project.ID)
			paths[ms.Project.ID] = ms.NamespaceSlug + "/" + ms.Project.Slug
		}
		if len(ids) == 0 {
			return nil
		}
		//nolint:gosec // selectCols comes from the groupColumns allowlist, never from the caller
		rows, err := tx.Query(ctx,
			`SELECT `+selectCols+`,
			        sum(l.seconds), sum(l.tokens_in), sum(l.tokens_out),
			        sum(l.cache_read_tokens), sum(l.cache_write_tokens), sum(l.turns)
			 FROM usage_ledger l
			 WHERE l.project_id = ANY($1) AND l.day BETWEEN $2 AND $3
			 GROUP BY `+selectCols+`
			 ORDER BY `+selectCols,
			ids, from.Format(time.DateOnly), to.Format(time.DateOnly))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r UsageRow
			dest := make([]any, 0, len(dims)+6)
			var day time.Time
			var project, principal uuid.UUID
			var model, branch, ticket string
			for _, g := range dims {
				switch g {
				case GroupProject:
					dest = append(dest, &project)
				case GroupDay:
					dest = append(dest, &day)
				case GroupPrincipal:
					dest = append(dest, &principal)
				case GroupModel:
					dest = append(dest, &model)
				case GroupBranch:
					dest = append(dest, &branch)
				case GroupTicket:
					dest = append(dest, &ticket)
				}
			}
			dest = append(dest, &r.Seconds, &r.Tokens.TokensIn, &r.Tokens.TokensOut,
				&r.Tokens.CacheReadTokens, &r.Tokens.CacheWriteTokens, &r.Turns)
			if err := rows.Scan(dest...); err != nil {
				return err
			}
			r.Tokens.Model = model
			if asked[GroupProject] {
				r.ProjectID = &project
			}
			if asked[GroupDay] {
				r.Day = &day
			}
			if asked[GroupPrincipal] {
				r.PrincipalID = &principal
			}
			if asked[GroupModel] {
				r.Model = &model
			}
			if asked[GroupBranch] {
				r.Branch = &branch
			}
			if asked[GroupTicket] {
				r.TicketKey = &ticket
			}
			raw = append(raw, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if asked[GroupPrincipal] {
			return nameRows(ctx, tx, raw)
		}
		return nil
	})
	if err != nil {
		return UsageSummary{}, fmt.Errorf("plans: usage: %w", err)
	}
	return s.foldUsage(raw, asked, paths), nil
}

// nameRows fills in who each principal is: a person's display name or an
// agent's name.
func nameRows(ctx context.Context, tx pgx.Tx, rows []UsageRow) error {
	ids := map[uuid.UUID]bool{}
	for _, r := range rows {
		ids[*r.PrincipalID] = true
	}
	list := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	res, err := tx.Query(ctx,
		`SELECT p.id, p.kind, COALESCE(u.display_name, sp.name, '')
		 FROM principals p
		 LEFT JOIN users u ON u.id = p.user_id
		 LEFT JOIN service_principals sp ON sp.id = p.service_principal_id
		 WHERE p.id = ANY($1)`, list)
	if err != nil {
		return err
	}
	defer res.Close()
	type who struct{ kind, name string }
	names := map[uuid.UUID]who{}
	for res.Next() {
		var id uuid.UUID
		var w who
		if err := res.Scan(&id, &w.kind, &w.name); err != nil {
			return err
		}
		names[id] = w
	}
	if err := res.Err(); err != nil {
		return err
	}
	for i := range rows {
		w := names[*rows[i].PrincipalID]
		rows[i].PrincipalKind, rows[i].PrincipalName = w.kind, w.name
	}
	return nil
}

// foldUsage prices each row, merges rows that differ only by model when the
// model was not asked for, and adds up the total.
func (s *Service) foldUsage(raw []UsageRow, asked map[string]bool, paths map[uuid.UUID]string) UsageSummary {
	out := UsageSummary{}
	if s.prices != nil {
		out.Currency, out.AsOf = s.prices.Currency, s.prices.AsOf
	}
	unpriced := map[string]bool{}
	price := func(t Tokens) *float64 {
		if s.prices == nil || !t.any() {
			return nil
		}
		c, ok := s.prices.Cost(t)
		if !ok {
			unpriced[t.Model] = true
			return nil
		}
		return &c
	}
	addCost := func(dst **float64, c *float64) {
		if c == nil {
			return
		}
		if *dst == nil {
			v := 0.0
			*dst = &v
		}
		**dst += *c
	}

	merged := map[string]int{}
	var rows []UsageRow
	if s.prices != nil {
		zero := 0.0
		out.Total.Cost = &zero
	}
	for _, r := range raw {
		c := price(r.Tokens)
		out.Total.Seconds += r.Seconds
		out.Total.Turns += r.Turns
		out.Total.Tokens.add(r.Tokens)
		addCost(&out.Total.Cost, c)

		if r.ProjectID != nil {
			r.ProjectPath = paths[*r.ProjectID]
		}
		r.Cost = c
		if asked[GroupModel] {
			rows = append(rows, r)
			continue
		}
		k := rowKey(r)
		if i, ok := merged[k]; ok {
			rows[i].Seconds += r.Seconds
			rows[i].Turns += r.Turns
			rows[i].Tokens.add(r.Tokens)
			addCost(&rows[i].Cost, c)
			continue
		}
		r.Tokens.Model = ""
		merged[k] = len(rows)
		rows = append(rows, r)
	}
	if len(rows) > MaxUsageRows {
		rows, out.Truncated = rows[:MaxUsageRows], true
	}
	out.Rows = rows
	if out.Rows == nil {
		out.Rows = []UsageRow{}
	}
	for m := range unpriced {
		out.Unpriced = append(out.Unpriced, m)
	}
	sort.Strings(out.Unpriced)
	return out
}

func rowKey(r UsageRow) string {
	var b strings.Builder
	if r.ProjectID != nil {
		b.WriteString(r.ProjectID.String())
	}
	b.WriteByte(0)
	if r.Day != nil {
		b.WriteString(r.Day.Format(time.DateOnly))
	}
	b.WriteByte(0)
	if r.PrincipalID != nil {
		b.WriteString(r.PrincipalID.String())
	}
	b.WriteByte(0)
	if r.Branch != nil {
		b.WriteString(*r.Branch)
	}
	b.WriteByte(0)
	if r.TicketKey != nil {
		b.WriteString(*r.TicketKey)
	}
	return b.String()
}
