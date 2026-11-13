package memories

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func hit(id uuid.UUID, match string) Hit { return Hit{Memory: Memory{ID: id}, Match: match} }

// Reciprocal rank fusion: a memory both rankings place well beats one only
// a single ranking places first, and a memory found by text keeps the text
// hit's match.
func TestFuseRanksByReciprocalRank(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	text := []Hit{hit(a, MatchTitle), hit(b, MatchBody), hit(c, MatchBody)}
	meaning := []Hit{hit(d, MatchSemantic), hit(b, MatchSemantic), hit(a, MatchSemantic)}
	got := Fuse(text, meaning)
	if len(got) != 4 {
		t.Fatalf("fused %d hits, want 4", len(got))
	}
	// a: 1/61 + 1/63 = 0.03227; b: 2/62 = 0.03226; d: 1/61; c: 1/63.
	order := []uuid.UUID{a, b, d, c}
	for i, id := range order {
		if got[i].Memory.ID != id {
			t.Fatalf("position %d is %v, want %v (%+v)", i, got[i].Memory.ID, id, got)
		}
	}
	if got[0].Match != MatchTitle || got[1].Match != MatchBody || got[2].Match != MatchSemantic {
		t.Fatalf("matches %s %s %s: a text match must survive fusion", got[0].Match, got[1].Match, got[2].Match)
	}
	if !(got[0].Score > got[1].Score && got[1].Score > got[2].Score && got[2].Score > got[3].Score) {
		t.Fatalf("scores not descending: %+v", got)
	}
	if len(Fuse()) != 0 || len(Fuse(nil, nil)) != 0 {
		t.Fatal("fusing nothing found something")
	}
}

type stubSemantic struct{ err error }

func (s stubSemantic) Rank(context.Context, uuid.UUID, string, []Memory) ([]Hit, error) {
	return nil, s.err
}
func (s stubSemantic) Similar(context.Context, uuid.UUID, Memory, []Memory, int) ([]Hit, error) {
	return nil, s.err
}
func (stubSemantic) Enqueue(uuid.UUID, uuid.UUID, uuid.UUID) {}

func TestModesFollowConfiguration(t *testing.T) {
	s := &Service{ranker: TextRanker{}}
	if s.DefaultMode() != ModeText || len(s.Modes()) != 1 {
		t.Fatalf("without a provider: default %s, modes %v", s.DefaultMode(), s.Modes())
	}
	for _, m := range []string{ModeSemantic, ModeHybrid} {
		if _, err := s.resolveMode(m); !errors.Is(err, ErrSemanticOff) || !errors.Is(err, ErrInvalid) {
			t.Fatalf("mode %s without a provider: %v, want ErrSemanticOff wrapping ErrInvalid", m, err)
		}
	}
	if _, err := s.resolveMode("fuzzy"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown mode: %v", err)
	}
	s.EnableSemantic(stubSemantic{})
	if got, _ := s.resolveMode(""); got != ModeHybrid || len(s.Modes()) != 3 {
		t.Fatalf("with a provider: default %s, modes %v", got, s.Modes())
	}
}

// A hybrid search whose semantic half fails answers by text and says so;
// a semantic search fails outright, because it was asked for by name.
func TestHybridFallsBackToText(t *testing.T) {
	s := &Service{ranker: TextRanker{}}
	s.EnableSemantic(stubSemantic{err: errors.New("provider down")})
	cands := []Memory{{ID: uuid.New(), Title: "Webhook retries"}}
	hits, _, degraded, err := s.rank(context.Background(), uuid.New(), "webhook", ModeHybrid, cands)
	if err != nil || !degraded || len(hits) != 1 || hits[0].Match != MatchTitle {
		t.Fatalf("hybrid with a failing provider: hits %+v degraded %v err %v", hits, degraded, err)
	}
	if _, _, _, err := s.rank(context.Background(), uuid.New(), "webhook", ModeSemantic, cands); err == nil {
		t.Fatal("a semantic search with a failing provider succeeded")
	}
}
