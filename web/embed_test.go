package web

import "testing"

// The binary must always carry a UI, even when nobody has run Node. A
// committed placeholder dist/index.html is what makes `go build ./cmd/ccd`
// work on a clean checkout; this test is what notices if it goes missing.
func TestDistFSContainsIndexHTML(t *testing.T) {
	fsys, err := DistFS()
	if err != nil {
		t.Fatalf("DistFS: %v", err)
	}
	if _, err := fsys.Open("index.html"); err != nil {
		t.Fatalf("open index.html: %v", err)
	}
}
