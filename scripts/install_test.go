package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// fakeRelease serves what install.sh reads: the two release endpoints of the
// GitHub API and the release download URLs.
type fakeRelease struct {
	mu       sync.Mutex        // the test edits the release between runs
	latest   string            // body of /releases/latest; "" answers 404
	releases string            // body of /releases
	files    map[string][]byte // "/<tag>/<asset>" -> body
}

func (f *fakeRelease) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const repo = "/repos/koduj-dev/context-commander"
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == repo+"/releases/latest" && f.latest != "":
		_, _ = w.Write([]byte(f.latest))
	case r.URL.Path == repo+"/releases":
		_, _ = w.Write([]byte(f.releases))
	case strings.HasPrefix(r.URL.Path, "/koduj-dev/context-commander/releases/download/"):
		if b, ok := f.files[strings.TrimPrefix(r.URL.Path, "/koduj-dev/context-commander/releases/download")]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	default:
		http.NotFound(w, r)
	}
}

// publish adds tag's assets: a "binary" (a script printing the tag), a man
// tarball when man is set, and a SHA256SUMS over them (sums overrides it).
func (f *fakeRelease) publish(t *testing.T, tag string, man bool, sums string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	asset := "ccd-" + runtime.GOOS + "-" + runtime.GOARCH
	bin := []byte("#!/bin/sh\necho " + tag + "\n")
	f.files["/"+tag+"/"+asset] = bin
	f.files["/"+tag+"/SHA256SUMS.bundle"] = []byte("{}")
	lines := sumLine(bin, asset)
	if man {
		tgz := manTarball(t, "ccd.1.gz", "ccd-serve.1.gz")
		f.files["/"+tag+"/ccd-man.tar.gz"] = tgz
		lines += sumLine(tgz, "ccd-man.tar.gz")
	}
	if sums != "" {
		lines = sums
	}
	f.files["/"+tag+"/SHA256SUMS"] = []byte(lines)
}

func (f *fakeRelease) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func sumLine(b []byte, name string) string {
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]) + "  " + name + "\n"
}

func manTarball(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		body := []byte("page " + n)
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(body)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// GitHub's compact JSON, trimmed to the fields that matter and with a body
// that tries to look like a draft:false release to a careless parser.
func releaseJSON(tag string, draft, pre bool) string {
	return fmt.Sprintf(`{"url":"x","id":1,"author":{"login":"bot","type":"Bot"},"tag_name":%q,"name":%q,"draft":%t,"immutable":false,"prerelease":%t,"assets":[{"name":"ccd-linux-amd64","uploader":{"login":"bot"}}],"body":"see \"tag_name\":\"v6.6.6\", \"draft\":false"}`, tag, tag, draft, pre)
}

type installRun struct {
	out    string
	err    error
	binDir string
}

// scriptTools are the external commands install.sh may run. Missing ones
// (sha256sum on macOS, say) are simply not linked; the script falls back.
var scriptTools = []string{
	"awk", "basename", "cat", "chmod", "cp", "curl", "dirname", "grep", "gzip", "id",
	"mkdir", "mktemp", "mv", "openssl", "rm", "sha256sum", "shasum", "tar", "tr", "uname",
}

// runInstall runs install.sh against srv with args. A fake cosign that exits
// with cosignExit is put on PATH unless cosignExit is -1.
func runInstall(t *testing.T, srv *httptest.Server, cosignExit int, args ...string) installRun {
	t.Helper()
	root := t.TempDir()
	binDir := filepath.Join(root, "prefix", "bin")
	fake := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(fake, 0o750); err != nil {
		t.Fatal(err)
	}
	if cosignExit >= 0 {
		script := fmt.Sprintf("#!/bin/sh\nexit %d\n", cosignExit)
		if err := os.WriteFile(filepath.Join(fake, "cosign"), []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
			t.Fatal(err)
		}
	}
	// PATH is the fixture directory alone: links to just the tools the
	// script uses, so a real cosign (or anything else) installed on this
	// machine cannot change what the script does.
	for _, tool := range scriptTools {
		if real, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(real, filepath.Join(fake, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := fake
	cmd := exec.CommandContext(t.Context(), "sh", append([]string{"install.sh", "--bin-dir", binDir}, args...)...) //nolint:gosec // the repo's own script with test arguments
	cmd.Env = []string{"PATH=" + path, "HOME=" + root, "CCD_GITHUB_API=" + srv.URL, "CCD_GITHUB_URL=" + srv.URL}
	out, err := cmd.CombinedOutput()
	return installRun{out: string(out), err: err, binDir: binDir}
}

func TestInstallScript(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("install.sh is for Linux and macOS")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	f := &fakeRelease{files: map[string][]byte{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	// Only pre-releases (and a newer draft) exist: the default finds the
	// newest published pre-release, and installs its man pages beside bin/.
	f.publish(t, "v0.1.0-beta.1", false, "")
	f.publish(t, "v0.1.0-beta.2", true, "")
	f.set(func() {
		f.releases = "[" + releaseJSON("v0.2.0", true, false) + "," + releaseJSON("v0.1.0-beta.2", false, true) + "," + releaseJSON("v0.1.0-beta.1", false, true) + "]"
	})
	r := runInstall(t, srv, -1)
	if r.err != nil {
		t.Fatalf("%v\n%s", r.err, r.out)
	}
	if got := run(t, filepath.Join(r.binDir, "ccd")); got != "v0.1.0-beta.2" {
		t.Errorf("installed %q, want v0.1.0-beta.2\n%s", got, r.out)
	}
	for _, want := range []string{"no stable release yet", "cosign not found", "2 manual pages"} {
		if !strings.Contains(r.out, want) {
			t.Errorf("output lacks %q:\n%s", want, r.out)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.binDir), "share", "man", "man1", "ccd-serve.1.gz")); err != nil {
		t.Errorf("man page not installed: %v", err)
	}

	// A stable release wins over a newer pre-release unless --pre is given.
	f.publish(t, "v0.1.0", false, "")
	f.set(func() { f.latest = releaseJSON("v0.1.0", false, false) })
	if r := runInstall(t, srv, 0); r.err != nil || run(t, filepath.Join(r.binDir, "ccd")) != "v0.1.0" || !strings.Contains(r.out, "signature verified") {
		t.Errorf("default with a stable release: %v\n%s", r.err, r.out)
	}
	if r := runInstall(t, srv, -1, "--pre"); r.err != nil || run(t, filepath.Join(r.binDir, "ccd")) != "v0.1.0-beta.2" {
		t.Errorf("--pre: %v\n%s", r.err, r.out)
	}
	if r := runInstall(t, srv, -1, "--version", "0.1.0-beta.1"); r.err != nil || run(t, filepath.Join(r.binDir, "ccd")) != "v0.1.0-beta.1" {
		t.Errorf("--version: %v\n%s", r.err, r.out)
	}

	// Refusals: nothing may be installed.
	asset := "ccd-" + runtime.GOOS + "-" + runtime.GOARCH
	f.publish(t, "v7.7.7", true, sumLine([]byte("#!/bin/sh\necho v7.7.7\n"), asset)+strings.Repeat("ab", 32)+"  ccd-man.tar.gz\n")
	f.publish(t, "v6.6.6", false, strings.Repeat("ab", 32)+"  ccd-"+runtime.GOOS+"-"+runtime.GOARCH+"\n")
	for name, c := range map[string]struct {
		cosign int
		args   []string
		want   string
	}{
		"checksum mismatch": {-1, []string{"--version", "v6.6.6"}, "checksum mismatch"},
		"bad signature":     {1, nil, "could not verify the signature"},
		"signature missing": {-1, []string{"--require-signature"}, "cosign is not on PATH"},
		"not a tag":         {-1, []string{"--version", "v1.0.0/../../x"}, "not a release tag"},
		"no such release":   {-1, []string{"--version", "v9.9.9"}, "cannot download SHA256SUMS"},
		// The man bundle is verified before the binary is installed, too.
		"man bundle mismatch": {-1, []string{"--version", "v7.7.7"}, "checksum mismatch for ccd-man.tar.gz"},
	} {
		r := runInstall(t, srv, c.cosign, c.args...)
		if r.err == nil || !strings.Contains(r.out, c.want) {
			t.Errorf("%s: err %v, want %q in:\n%s", name, r.err, c.want, r.out)
		}
		if _, err := os.Stat(filepath.Join(r.binDir, "ccd")); err == nil {
			t.Errorf("%s: installed a binary anyway", name)
		}
	}

	// A man directory it cannot write fails before the binary is touched.
	if os.Geteuid() != 0 {
		ro := filepath.Join(t.TempDir(), "man1")
		if err := os.Mkdir(ro, 0o500); err != nil {
			t.Fatal(err)
		}
		r := runInstall(t, srv, -1, "--version", "v0.1.0-beta.2", "--man-dir", ro)
		if r.err == nil || !strings.Contains(r.out, "is not writable") {
			t.Errorf("read-only --man-dir: err %v\n%s", r.err, r.out)
		}
		if _, err := os.Stat(filepath.Join(r.binDir, "ccd")); err == nil {
			t.Error("read-only --man-dir: installed the binary anyway")
		}
	}
}

func run(t *testing.T, exe string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), exe).Output() //nolint:gosec // the fixture just installed
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSpace(string(out))
}
