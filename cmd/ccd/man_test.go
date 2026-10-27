package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The pages are generated from the live command tree, so they carry the
// version stamped into the binary and every flag --help shows, and the
// build-only `man` command documents nothing about itself.
func TestManPages(t *testing.T) {
	t.Setenv("SOURCE_DATE_EPOCH", "1798329600")
	version = "v9.8.7-test"
	t.Cleanup(func() { version = "dev" })

	dir := t.TempDir()
	root := newRootCmd()
	root.SetArgs([]string{"man", dir})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}

	read := func(name string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a path in t.TempDir()
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	top := read("ccd.1")
	for _, want := range []string{"Context Commander v9.8.7-test", "/etc/context-commander/ccd.env", "ccd-serve(1)"} {
		if !strings.Contains(top, want) {
			t.Errorf("ccd.1 is missing %q", want)
		}
	}
	if !strings.Contains(read("ccd-serve.1"), "--storage-quota") {
		t.Error("ccd-serve.1 does not document --storage-quota")
	}
	if _, err := os.Stat(filepath.Join(dir, "ccd-man.1")); err == nil {
		t.Error("the hidden man command got a page of its own")
	}
	if strings.Contains(top, "Auto generated") {
		t.Error("pages carry cobra's dated auto-generated footer, so rebuilds differ")
	}
}
