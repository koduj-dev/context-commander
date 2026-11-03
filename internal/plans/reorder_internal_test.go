package plans

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestMoveToPutsTheIDAtTheOneBasedPlaceAndClamps(t *testing.T) {
	a, b, c, d := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ids := []uuid.UUID{a, b, c, d}
	cases := []struct {
		name      string
		id        uuid.UUID
		to        int
		want      []uuid.UUID
		wantPlace int
	}{
		{"to the front", c, 1, []uuid.UUID{c, a, b, d}, 1},
		{"one down", a, 2, []uuid.UUID{b, a, c, d}, 2},
		{"one up", c, 2, []uuid.UUID{a, c, b, d}, 2},
		{"to the end", a, 4, []uuid.UUID{b, c, d, a}, 4},
		{"past the end is last", b, 99, []uuid.UUID{a, c, d, b}, 4},
		{"below one is first", d, 0, []uuid.UUID{d, a, b, c}, 1},
		{"where it already is", b, 2, []uuid.UUID{a, b, c, d}, 2},
	}
	for _, tc := range cases {
		got, place, ok := moveTo(ids, tc.id, tc.to)
		if !ok || place != tc.wantPlace {
			t.Errorf("%s: ok=%v place=%d, want place %d", tc.name, ok, place, tc.wantPlace)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: order %v, want %v", tc.name, got, tc.want)
				break
			}
		}
	}
	if ids[0] != a || ids[3] != d {
		t.Error("moveTo changed its input")
	}
	if _, _, ok := moveTo(ids, uuid.New(), 1); ok {
		t.Error("an id not in the list was moved")
	}
}

func TestChangesReachOnlyTheStreamsThatAskedAndCoalesce(t *testing.T) {
	c := newChanges()
	p1, p2, cred := uuid.New(), uuid.New(), uuid.New()
	board, err := c.subscribe(cred, []uuid.UUID{p1}, false)
	if err != nil {
		t.Fatal(err)
	}
	dash, err := c.subscribe(cred, []uuid.UUID{p1, p2}, true)
	if err != nil {
		t.Fatal(err)
	}

	c.publish(p1, false)
	c.publish(p1, false)
	c.publish(p2, false)
	if n := drain(board.C()); n != 1 {
		t.Errorf("board stream got %d notifications for a burst, want 1 (coalesced)", n)
	}
	if n := drain(dash.C()); n != 0 {
		t.Errorf("presence stream was told about a change that was not presence")
	}
	c.publish(p2, true)
	if n := drain(board.C()); n != 0 {
		t.Error("board stream was told about another project")
	}
	if n := drain(dash.C()); n != 1 {
		t.Error("presence stream missed a presence change")
	}

	// The cap is per credential, and a closed stream gives its place back.
	board.Close()
	board.Close()
	var held []*Subscription
	for range StreamsPerCredential - 1 {
		s, err := c.subscribe(cred, nil, false)
		if err != nil {
			t.Fatalf("under the cap: %v", err)
		}
		held = append(held, s)
	}
	if _, err := c.subscribe(cred, nil, false); !errors.Is(err, ErrTooManyStreams) {
		t.Errorf("over the cap: %v, want ErrTooManyStreams", err)
	}
	if _, err := c.subscribe(uuid.New(), nil, false); err != nil {
		t.Errorf("another credential was refused: %v", err)
	}

	c.close()
	select {
	case <-dash.Done():
	default:
		t.Error("closing did not end the streams")
	}
	if _, err := c.subscribe(uuid.New(), nil, false); !errors.Is(err, ErrShuttingDown) {
		t.Errorf("after close: %v, want ErrShuttingDown", err)
	}
	for _, s := range held {
		s.Close()
	}
}

func drain(c <-chan struct{}) int {
	n := 0
	for {
		select {
		case <-c:
			n++
		default:
			return n
		}
	}
}
