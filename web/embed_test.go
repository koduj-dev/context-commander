package web

import "testing"

func TestDistFSContainsIndexHTML(t *testing.T) {
	fsys, err := DistFS()
	if err != nil {
		t.Fatalf("DistFS: %v", err)
	}
	if _, err := fsys.Open("index.html"); err != nil {
		t.Fatalf("open index.html: %v", err)
	}
}
