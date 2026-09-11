package paging_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/koduj-dev/context-commander/internal/paging"
)

func TestClampedLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, paging.DefaultLimit},
		{-1, paging.DefaultLimit},
		{1, 1},
		{paging.MaxLimit, paging.MaxLimit},
		{paging.MaxLimit + 1, paging.MaxLimit},
		{1 << 30, paging.MaxLimit},
	}
	for _, tc := range cases {
		if got := (paging.Options{Limit: tc.in}).ClampedLimit(); got != tc.want {
			t.Errorf("Options{Limit: %d}.ClampedLimit() = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestMoreTrimsToTheProbeRow(t *testing.T) {
	rows := []int{1, 2, 3, 4}

	page, hasMore := paging.More(rows, 3)
	if !hasMore || len(page) != 3 {
		t.Errorf("More(4 rows, limit 3) = %v, %v; want 3 rows and hasMore", page, hasMore)
	}

	page, hasMore = paging.More(rows, 4)
	if hasMore || len(page) != 4 {
		t.Errorf("More(4 rows, limit 4) = %v, %v; want all 4 and no more", page, hasMore)
	}

	page, hasMore = paging.More([]int(nil), 10)
	if hasMore || len(page) != 0 {
		t.Errorf("More(no rows) = %v, %v; want empty and no more", page, hasMore)
	}
}

func TestTimeUUIDRoundTrip(t *testing.T) {
	// Microsecond precision, matching what Postgres timestamptz stores and
	// hands back — a cursor built from a value read out of the database.
	want := time.Date(2026, 9, 11, 16, 40, 5, 123456000, time.UTC)
	wantID := uuid.New()

	gotTime, gotID, ok, err := (paging.Options{Cursor: paging.EncodeTimeUUID(want, wantID)}).DecodeTimeUUID()
	if err != nil || !ok {
		t.Fatalf("DecodeTimeUUID: ok=%v err=%v", ok, err)
	}
	if !gotTime.Equal(want) {
		t.Errorf("time = %v, want %v", gotTime, want)
	}
	if gotID != wantID {
		t.Errorf("id = %v, want %v", gotID, wantID)
	}
}

func TestSeqAndStringRoundTrip(t *testing.T) {
	seq, ok, err := (paging.Options{Cursor: paging.EncodeSeq(9007199254740993)}).DecodeSeq()
	if err != nil || !ok || seq != 9007199254740993 {
		t.Errorf("DecodeSeq = %d, %v, %v", seq, ok, err)
	}

	// A secret name may contain the separator; the payload must survive it.
	const name = "weird|name|with|pipes"
	key, ok, err := (paging.Options{Cursor: paging.EncodeString(name)}).DecodeString()
	if err != nil || !ok || key != name {
		t.Errorf("DecodeString = %q, %v, %v", key, ok, err)
	}
}

func TestEmptyCursorIsTheFirstPageNotAnError(t *testing.T) {
	if _, _, ok, err := (paging.Options{}).DecodeTimeUUID(); ok || err != nil {
		t.Errorf("DecodeTimeUUID(empty) = ok %v, err %v; want false, nil", ok, err)
	}
	if _, ok, err := (paging.Options{}).DecodeSeq(); ok || err != nil {
		t.Errorf("DecodeSeq(empty) = ok %v, err %v; want false, nil", ok, err)
	}
	if _, ok, err := (paging.Options{}).DecodeString(); ok || err != nil {
		t.Errorf("DecodeString(empty) = ok %v, err %v; want false, nil", ok, err)
	}
}

// TestCursorFromAnotherListIsRejected is the reason each cursor carries its
// kind: a cursor pasted from a differently-ordered endpoint must fail
// cleanly instead of decoding into a position that means something else
// there.
func TestCursorFromAnotherListIsRejected(t *testing.T) {
	timeCursor := paging.EncodeTimeUUID(time.Now(), uuid.New())
	seqCursor := paging.EncodeSeq(42)
	stringCursor := paging.EncodeString("api-token")

	if _, _, err := (paging.Options{Cursor: timeCursor}).DecodeSeq(); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("time cursor decoded as seq: err = %v", err)
	}
	if _, _, _, err := (paging.Options{Cursor: seqCursor}).DecodeTimeUUID(); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("seq cursor decoded as time+uuid: err = %v", err)
	}
	if _, _, err := (paging.Options{Cursor: stringCursor}).DecodeSeq(); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("string cursor decoded as seq: err = %v", err)
	}
	if _, _, _, err := (paging.Options{Cursor: "not base64!!"}).DecodeTimeUUID(); !errors.Is(err, paging.ErrInvalidCursor) {
		t.Errorf("garbage cursor: err = %v", err)
	}
}
