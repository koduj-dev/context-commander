package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// fakeGitHub serves the two API endpoints self-update reads and the assets
// of whatever releases it holds, and records which assets were downloaded.
type fakeGitHub struct {
	t        *testing.T
	srv      *httptest.Server
	releases []ghRelease       // newest first, as GitHub lists them
	files    map[string][]byte // download path -> body
	mu       sync.Mutex
	fetched  []string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/repos/" + Repo + "/releases/latest":
		for _, rel := range f.releases {
			if !rel.Draft && !rel.Prerelease {
				_ = json.NewEncoder(w).Encode(rel)
				return
			}
		}
		http.NotFound(w, r)
	case "/repos/" + Repo + "/releases":
		_ = json.NewEncoder(w).Encode(f.releases)
	default:
		body, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f.mu.Lock()
		f.fetched = append(f.fetched, filepath.Base(r.URL.Path))
		f.mu.Unlock()
		_, _ = w.Write(body)
	}
}

// addRelease publishes tag with a binary for this platform, a SHA256SUMS
// covering it and a signature bundle. sums overrides the SHA256SUMS body.
func (f *fakeGitHub) addRelease(tag string, pre bool, binary []byte, sums *string) {
	name := "ccd-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	digest := sha256.Sum256(binary)
	body := fmt.Sprintf("%s  %s\n%s  ccd.sbom.spdx.json\n", hex.EncodeToString(digest[:]), name, strings.Repeat("0", 64))
	if sums != nil {
		body = *sums
	}
	rel := ghRelease{TagName: tag, Prerelease: pre, HTMLURL: "https://example.invalid/" + tag}
	for _, a := range []struct {
		name string
		body []byte
	}{{name, binary}, {sumsName, []byte(body)}, {bundleName, []byte("{}")}} {
		p := "/download/" + tag + "/" + a.name
		f.files[p] = a.body
		rel.Assets = append(rel.Assets, ghAsset{Name: a.name, URL: f.srv.URL + p, Size: int64(len(a.body))})
	}
	f.releases = append([]ghRelease{rel}, f.releases...)
}

func (f *fakeGitHub) downloaded(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.fetched {
		if strings.HasPrefix(n, name) {
			return true
		}
	}
	return false
}

// updater points at gh, replaces exe and finds no cosign.
func (f *fakeGitHub) updater(exe string) *Updater {
	u := New()
	u.APIBase = f.srv.URL
	u.Client = f.srv.Client()
	u.Executable = func() (string, error) { return exe, nil }
	u.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	u.Exists = func(string) bool { return false }
	return u
}

func oldBinary(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "ccd")
	if err := os.WriteFile(exe, []byte("OLD"), 0o750); err != nil { //nolint:gosec // a test file in t.TempDir()
		t.Fatal(err)
	}
	return exe
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p) //nolint:gosec // a path in t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	gh := newFakeGitHub(t)
	gh.addRelease("v0.1.0-beta.1", true, []byte("b1"), nil)
	u := gh.updater("/x/ccd")

	// Only a pre-release exists: the default still finds it, and says why.
	rel, note, err := u.resolve(ctx, false)
	if err != nil || rel.TagName != "v0.1.0-beta.1" || note == "" {
		t.Fatalf("pre-release only: %+v %q %v", rel, note, err)
	}

	gh.addRelease("v0.1.0", false, []byte("s"), nil)
	gh.addRelease("v0.2.0-rc.1", true, []byte("rc"), nil)
	gh.releases = append([]ghRelease{{TagName: "v9.0.0", Draft: true}}, gh.releases...)

	if rel, note, _ := u.resolve(ctx, false); rel.TagName != "v0.1.0" || note != "" {
		t.Errorf("default = %s %q, want the stable v0.1.0", rel.TagName, note)
	}
	if rel, _, _ := u.resolve(ctx, true); rel.TagName != "v0.2.0-rc.1" {
		t.Errorf("--pre = %s, want the newest non-draft v0.2.0-rc.1", rel.TagName)
	}
}

func TestRunInstalls(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("v0.1.0-beta.2", true, []byte("NEW BINARY"), nil)
	exe := oldBinary(t)

	var out bytes.Buffer
	if err := gh.updater(exe).Run(context.Background(), Options{Current: "v0.1.0-beta.1"}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if got := readFile(t, exe); got != "NEW BINARY" {
		t.Errorf("exe = %q, want the new binary", got)
	}
	if fi, _ := os.Stat(exe); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o750 {
		t.Errorf("mode = %v, want the old binary's 0750", fi.Mode().Perm())
	}
	for _, want := range []string{"No stable release yet", "Checksum OK", "Updated v0.1.0-beta.1 -> v0.1.0-beta.2", "cosign not found"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("left files behind next to the binary: %v", entries)
	}
}

func TestRunUpToDateAndCheck(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("v0.1.0", false, []byte("NEW"), nil)
	exe := oldBinary(t)
	u := gh.updater(exe)

	for _, current := range []string{"v0.1.0", "v0.2.0-rc.1"} {
		var out bytes.Buffer
		if err := u.Run(context.Background(), Options{Current: current}, &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Already up to date") {
			t.Errorf("%s: %s", current, out.String())
		}
	}

	var out bytes.Buffer
	if err := u.Run(context.Background(), Options{Current: "v0.1.0-beta.1", CheckOnly: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Update available: v0.1.0-beta.1 -> v0.1.0") {
		t.Errorf("--check: %s", out.String())
	}
	if readFile(t, exe) != "OLD" || gh.downloaded("ccd-") || gh.downloaded(sumsName) {
		t.Error("--check downloaded or replaced something")
	}
}

// Every way the checksum can fail to vouch for the download leaves the old
// binary in place and nothing beside it.
func TestRunRefusesUnverified(t *testing.T) {
	wrong := strings.Repeat("ab", 32) + "  ccd-" + runtime.GOOS + "-" + runtime.GOARCH + "\n"
	missing := strings.Repeat("ab", 32) + "  ccd-plan9-mips\n"
	cases := map[string]func(gh *fakeGitHub){
		"mismatch": func(gh *fakeGitHub) { gh.addRelease("v1.0.0", false, []byte("EVIL"), &wrong) },
		"no entry": func(gh *fakeGitHub) { gh.addRelease("v1.0.0", false, []byte("EVIL"), &missing) },
		"no SUMS":  func(gh *fakeGitHub) { gh.addRelease("v1.0.0", false, []byte("EVIL"), nil); gh.dropAsset(sumsName) },
		"bad digest": func(gh *fakeGitHub) {
			gh.addRelease("v1.0.0", false, []byte("NEW"), nil)
			gh.releases[0].Assets[0].Digest = "sha256:" + strings.Repeat("cd", 32)
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			gh := newFakeGitHub(t)
			setup(gh)
			exe := oldBinary(t)
			var out bytes.Buffer
			err := gh.updater(exe).Run(context.Background(), Options{Current: "v0.9.0"}, &out)
			if err == nil || !strings.Contains(err.Error(), "refusing to install") {
				t.Fatalf("err = %v, want a refusal\n%s", err, out.String())
			}
			if readFile(t, exe) != "OLD" {
				t.Error("the old binary was replaced")
			}
			if entries, _ := os.ReadDir(filepath.Dir(exe)); len(entries) != 1 {
				t.Errorf("left files behind: %v", entries)
			}
		})
	}
}

func (f *fakeGitHub) dropAsset(name string) {
	a := f.releases[0].Assets[:0]
	for _, x := range f.releases[0].Assets {
		if x.Name != name {
			a = append(a, x)
		}
	}
	f.releases[0].Assets = a
}

func TestRunPackageManaged(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.addRelease("v1.0.0", false, []byte("NEW"), nil)

	cases := []struct {
		exe, goos, probe, want string
	}{
		{"/opt/homebrew/Cellar/ccd/0.1.0/bin/ccd", "darwin", "", "brew upgrade ccd"},
		{"/home/linuxbrew/.linuxbrew/Cellar/ccd/0.1.0/bin/ccd", "linux", "", "brew upgrade ccd"},
		{"/usr/bin/ccd", "linux", "/var/lib/dpkg/info/ccd.list", "apt install --only-upgrade ccd"},
		{"/usr/bin/ccd", "linux", "/var/lib/rpm", "dnf upgrade ccd"},
		{"/usr/bin/ccd", "linux", "", "package manager"},
	}
	for _, c := range cases {
		u := gh.updater(c.exe)
		u.GOOS = c.goos
		u.Exists = func(p string) bool { return p == c.probe }
		err := u.Run(context.Background(), Options{Current: "v0.1.0"}, &bytes.Buffer{})
		var pm *ErrPackageManaged
		if !errors.As(err, &pm) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s (%s): err = %v, want a refusal naming %q", c.exe, c.probe, err, c.want)
		}
	}
	if len(gh.fetched) != 0 {
		t.Errorf("a refused update still downloaded %v", gh.fetched)
	}

	// --check still answers, and names the package manager's command.
	u := gh.updater("/opt/homebrew/Cellar/ccd/0.1.0/bin/ccd")
	var out bytes.Buffer
	if err := u.Run(context.Background(), Options{Current: "v0.1.0", CheckOnly: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "upgrade with: brew upgrade ccd") {
		t.Errorf("--check on Homebrew: %s", out.String())
	}

	// ~/.local/bin and /usr/local/bin are the installer's, not a package's.
	for _, exe := range []string{"/usr/local/bin/ccd", "/home/u/.local/bin/ccd"} {
		u := gh.updater(exe)
		u.GOOS = "linux"
		u.Exists = func(string) bool { return true }
		if pm := u.packageManager(exe); pm != nil {
			t.Errorf("%s reported as installed by %s", exe, pm.Manager)
		}
	}
}

func TestRunDevBuild(t *testing.T) {
	gh := newFakeGitHub(t)
	err := gh.updater(oldBinary(t)).Run(context.Background(), Options{Current: "dev"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "development build") {
		t.Errorf("err = %v, want a development-build refusal", err)
	}
}

func TestRunSignature(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake cosign is a shell script")
	}
	// A fake cosign that records its arguments and exits with $FAKE_COSIGN_EXIT.
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	cosign := filepath.Join(dir, "cosign")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\nexit ${FAKE_COSIGN_EXIT:-0}\n"
	if err := os.WriteFile(cosign, []byte(script), 0o700); err != nil { //nolint:gosec // an executable test fixture
		t.Fatal(err)
	}

	gh := newFakeGitHub(t)
	gh.addRelease("v1.0.0", false, []byte("NEW"), nil)

	t.Run("required but missing", func(t *testing.T) {
		exe := oldBinary(t)
		err := gh.updater(exe).Run(context.Background(), Options{Current: "v0.1.0", RequireSignature: true}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "cosign is not on PATH") || readFile(t, exe) != "OLD" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		t.Setenv("FAKE_COSIGN_EXIT", "1")
		exe := oldBinary(t)
		u := gh.updater(exe)
		u.LookPath = func(string) (string, error) { return cosign, nil }
		err := u.Run(context.Background(), Options{Current: "v0.1.0"}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "cosign could not verify") || readFile(t, exe) != "OLD" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("good signature", func(t *testing.T) {
		exe := oldBinary(t)
		u := gh.updater(exe)
		u.LookPath = func(string) (string, error) { return cosign, nil }
		var out bytes.Buffer
		if err := u.Run(context.Background(), Options{Current: "v0.1.0", RequireSignature: true}, &out); err != nil {
			t.Fatalf("%v\n%s", err, out.String())
		}
		if readFile(t, exe) != "NEW" {
			t.Error("not installed")
		}
		// The signature must be bound to this tag's release workflow.
		want := "https://github.com/" + Repo + "/.github/workflows/release.yml@refs/tags/v1.0.0"
		if args := readFile(t, argsFile); !strings.Contains(args, "--certificate-identity\n"+want+"\n") {
			t.Errorf("cosign args:\n%s\nwant identity %s", args, want)
		}
	})
}

// The writability check comes before any download.
func TestRunUnwritableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based permission denial does not apply")
	}
	gh := newFakeGitHub(t)
	gh.addRelease("v1.0.0", false, []byte("NEW"), nil)
	exe := oldBinary(t)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // making a test dir read-only
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // restoring it for TempDir's cleanup

	err := gh.updater(exe).Run(context.Background(), Options{Current: "v0.1.0"}, &bytes.Buffer{})
	var nw *ErrNotWritable
	if !errors.As(err, &nw) {
		t.Fatalf("err = %v, want ErrNotWritable", err)
	}
	if len(gh.fetched) != 0 {
		t.Errorf("downloaded %v into a directory it cannot write", gh.fetched)
	}
}

func TestSumFor(t *testing.T) {
	sums := []byte("aa  ccd-linux-amd64\nbb *ccd-darwin-arm64\ncc  ccd-linux-amd64.sig\n")
	if got, err := sumFor(sums, "ccd-darwin-arm64"); err != nil || got != "bb" {
		t.Errorf("binary-mode entry = %q, %v", got, err)
	}
	if got, _ := sumFor(sums, "ccd-linux-amd64"); got != "aa" {
		t.Errorf("exact name = %q", got)
	}
	if _, err := sumFor(sums, "ccd-linux"); err == nil {
		t.Error("a prefix matched")
	}
}
