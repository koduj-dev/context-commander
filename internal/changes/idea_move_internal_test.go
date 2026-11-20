package changes

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/ideas"
)

// An idea reports its latest move by someone else after since, and nothing
// when every move is the caller's own or older.
func TestIdeaMove(t *testing.T) {
	me, alice, planner, owner := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	since := time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)
	at := func(m int) *time.Time { t := since.Add(time.Duration(m) * time.Minute); return &t }

	for _, tc := range []struct {
		name   string
		idea   ideas.Idea
		detail string
		by     uuid.UUID
	}{
		{"added", ideas.Idea{Status: ideas.StatusOpen, CreatedAt: *at(1), CreatedBy: alice}, "added", alice},
		{"added before since", ideas.Idea{Status: ideas.StatusOpen, CreatedAt: *at(-1), CreatedBy: alice}, "", uuid.Nil},
		{"my own", ideas.Idea{Status: ideas.StatusOpen, CreatedAt: *at(1), CreatedBy: me}, "", uuid.Nil},
		{"proposed after added", ideas.Idea{Status: ideas.StatusProposed, CreatedAt: *at(1), CreatedBy: alice,
			ClaimedBy: &planner, ProposedAt: at(2)}, "proposed", planner},
		{"accepted", ideas.Idea{Status: ideas.StatusAccepted, CreatedAt: *at(-5), CreatedBy: alice,
			ClaimedBy: &planner, ProposedAt: at(1), DecidedBy: &owner, DecidedAt: at(3)}, "accepted", owner},
		{"rejected and reopened", ideas.Idea{Status: ideas.StatusOpen, CreatedAt: *at(-5), CreatedBy: alice,
			ClaimedBy: &planner, ProposedAt: at(-2), DecidedBy: &owner, DecidedAt: at(1)}, "rejected", owner},
		{"I decided, the proposal still shows", ideas.Idea{Status: ideas.StatusAccepted, CreatedAt: *at(-5), CreatedBy: alice,
			ClaimedBy: &planner, ProposedAt: at(1), DecidedBy: &me, DecidedAt: at(2)}, "proposed", planner},
	} {
		c, ok := ideaMove(tc.idea, me, since)
		if ok != (tc.detail != "") || c.Detail != tc.detail || (ok && c.By != tc.by) {
			t.Errorf("%s: got %+v, %v", tc.name, c, ok)
		}
	}
}
