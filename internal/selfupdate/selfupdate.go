// Package selfupdate implements `ccd self-update`: find the release to move
// to on GitHub, download the asset for this OS/arch, verify it, and swap it in
// for the running executable.
//
// Modelled on docker-commander's internal/selfupdate, and resolving and
// verifying a release exactly as scripts/install.sh does:
//
//   - Which release: the newest stable one (GitHub's "Latest"); while there is
//     no stable release, the newest pre-release; with Pre set, the newest
//     release, pre-releases included. Never one older than the running binary.
//   - Verification is mandatory. The asset must match its line in the
//     release's SHA256SUMS, and the digest GitHub records for it when there is
//     one. When cosign is on PATH (or RequireSignature is set), SHA256SUMS
//     itself must carry the keyless signature release.yml made for that tag.
//   - The new binary lands in a temp file in the executable's own directory, so
//     the swap is a rename on one filesystem: atomic, never a half-written ccd.
//   - A binary a package manager owns (the .deb/.rpm's /usr/bin/ccd, Homebrew's
//     Cellar) is left alone: replacing it behind the package manager's back
//     would be undone, or worse, by its next upgrade. The refusal names the
//     command that does upgrade it.
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	// Repo is the GitHub repository releases are published to.
	Repo        = "koduj-dev/context-commander"
	userAgent   = "ccd-self-update"
	httpTimeout = 5 * time.Minute
	oidcIssuer  = "https://token.actions.githubusercontent.com"
	sumsName    = "SHA256SUMS"
	bundleName  = "SHA256SUMS.bundle"
	lockName    = ".ccd-update.lock"
	// maxAssetBytes caps a download whose size the release does not state, so
	// an oversized body cannot fill the disk before the checksum is checked.
	maxAssetBytes = 512 << 20
)

// Options are what `ccd self-update` passes in.
type Options struct {
	Current          string // the running version, e.g. "v0.1.0-beta.1"
	Pre              bool   // follow pre-releases too
	CheckOnly        bool   // report, do not install
	RequireSignature bool   // fail unless cosign verifies SHA256SUMS
}

// Updater talks to GitHub and replaces the executable. The zero value is not
// usable; New returns one wired to the real GitHub, executable and PATH.
// Tests swap the fields.
type Updater struct {
	APIBase    string // GitHub API, "https://api.github.com"
	Client     *http.Client
	Executable func() (string, error)
	LookPath   func(string) (string, error) // finds cosign
	Exists     func(string) bool            // package-database probes
	GOOS       string
	GOARCH     string
}

// New returns an Updater for the real environment.
func New() *Updater {
	return &Updater{
		APIBase:    "https://api.github.com",
		Client:     &http.Client{Timeout: httpTimeout},
		Executable: os.Executable,
		LookPath:   exec.LookPath,
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
	}
}

// ErrNotWritable is returned before anything is downloaded when the
// executable's directory does not accept a write.
type ErrNotWritable struct {
	Dir string
	Err error
}

func (e *ErrNotWritable) Error() string {
	return fmt.Sprintf("cannot write to %s (run it with sudo, or as the user that owns the file): %v", e.Dir, e.Err)
}

func (e *ErrNotWritable) Unwrap() error { return e.Err }

// ErrPackageManaged is returned when a package manager owns the executable.
type ErrPackageManaged struct {
	Path    string
	Manager string // "Homebrew", "the ccd .deb package", ...
	Upgrade string // the command that upgrades it
}

func (e *ErrPackageManaged) Error() string {
	return fmt.Sprintf("%s was installed by %s; ccd self-update leaves it alone. Upgrade it with:\n  %s", e.Path, e.Manager, e.Upgrade)
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"` // "sha256:…", when GitHub computed one
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	HTMLURL    string    `json:"html_url"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// Run resolves the release to move to and, unless CheckOnly, installs it,
// writing progress to w.
func (u *Updater) Run(ctx context.Context, opts Options, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	if !semver.IsValid(opts.Current) {
		return fmt.Errorf("this ccd is a development build (version %q), not a release; install a release with scripts/install.sh instead", opts.Current)
	}
	exe, err := u.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	managed := u.packageManager(exe)
	// Refuse before touching the network: nothing to download for a binary
	// that will not be replaced.
	if managed != nil && !opts.CheckOnly {
		return managed
	}
	// What is on disk before the release is even resolved; the swap goes
	// ahead only if it is still this file (see commit).
	var before os.FileInfo
	if !opts.CheckOnly {
		if before, err = os.Stat(exe); err != nil {
			return err
		}
	}

	_, _ = fmt.Fprintf(w, "Current version: %s\n", opts.Current)
	rel, note, err := u.resolve(ctx, opts.Pre)
	if err != nil {
		return err
	}
	if note != "" {
		_, _ = fmt.Fprintln(w, note)
	}
	_, _ = fmt.Fprintf(w, "Latest release:  %s\n", rel.TagName)

	if semver.Compare(opts.Current, rel.TagName) >= 0 {
		_, _ = fmt.Fprintln(w, "Already up to date.")
		if semver.Prerelease(opts.Current) != "" && !opts.Pre {
			_, _ = fmt.Fprintln(w, "This is a pre-release; pass --pre to follow newer pre-releases too.")
		}
		return nil
	}

	if opts.CheckOnly {
		_, _ = fmt.Fprintf(w, "Update available: %s -> %s\n", opts.Current, rel.TagName)
		if rel.HTMLURL != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", rel.HTMLURL)
		}
		if managed != nil {
			_, _ = fmt.Fprintf(w, "Installed by %s; upgrade with: %s\n", managed.Manager, managed.Upgrade)
		} else {
			cmd := "ccd self-update"
			if opts.Pre {
				cmd += " --pre"
			}
			_, _ = fmt.Fprintf(w, "Run `%s` to install it.\n", cmd)
		}
		return nil
	}

	if err := u.install(ctx, exe, before, rel, opts.RequireSignature, w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "Updated %s -> %s (%s).\n", opts.Current, rel.TagName, exe)
	_, _ = fmt.Fprintln(w, "A running `ccd serve` keeps the old version until it is restarted.")
	return nil
}

// packageManager reports who owns exe, or nil when nobody does. The paths are
// the ones deploy/nfpm.yaml and the Homebrew formula install to.
func (u *Updater) packageManager(exe string) *ErrPackageManaged {
	slash := filepath.ToSlash(exe)
	if strings.Contains(slash, "/Cellar/") {
		return &ErrPackageManaged{Path: exe, Manager: "Homebrew", Upgrade: "brew upgrade ccd"}
	}
	if u.GOOS != "linux" {
		return nil
	}
	switch filepath.Dir(slash) {
	case "/usr/bin", "/bin", "/usr/sbin", "/sbin":
	default:
		return nil
	}
	switch {
	case u.Exists("/var/lib/dpkg/info/ccd.list"):
		return &ErrPackageManaged{Path: exe, Manager: "the ccd .deb package",
			Upgrade: "sudo apt update && sudo apt install --only-upgrade ccd   (or install the newer .deb from the release page)"}
	case u.Exists("/var/lib/rpm") || u.Exists("/usr/lib/sysimage/rpm"):
		return &ErrPackageManaged{Path: exe, Manager: "the ccd .rpm package",
			Upgrade: "sudo dnf upgrade ccd   (or install the newer .rpm from the release page)"}
	default:
		// /usr/bin is the package manager's even when we cannot tell which.
		return &ErrPackageManaged{Path: exe, Manager: "your system's package manager",
			Upgrade: "your package manager's upgrade command"}
	}
}

// resolve picks the release to move to (see the package comment). note
// explains a fallback to a pre-release.
func (u *Updater) resolve(ctx context.Context, pre bool) (rel *ghRelease, note string, err error) {
	if !pre {
		var latest ghRelease
		status, err := u.getJSON(ctx, "/repos/"+Repo+"/releases/latest", &latest)
		switch {
		case err != nil:
			return nil, "", err
		case status == http.StatusOK:
			if !semver.IsValid(latest.TagName) {
				return nil, "", fmt.Errorf("latest release has tag %q, not a version", latest.TagName)
			}
			return &latest, "", nil
		case status != http.StatusNotFound:
			return nil, "", fmt.Errorf("github API: latest release: HTTP %d", status)
		}
		note = "No stable release yet; using the newest pre-release."
	}
	var list []ghRelease
	status, err := u.getJSON(ctx, "/repos/"+Repo+"/releases?per_page=20", &list)
	if err != nil {
		return nil, "", err
	}
	if status != http.StatusOK {
		return nil, "", fmt.Errorf("github API: releases: HTTP %d", status)
	}
	// Newest first, as GitHub lists them; the same rule scripts/install.sh uses.
	for i := range list {
		if !list[i].Draft && semver.IsValid(list[i].TagName) {
			return &list[i], note, nil
		}
	}
	return nil, "", errors.New("found no release of " + Repo)
}

// getJSON GETs path from the API into v and returns the HTTP status; v is
// decoded only on 200.
func (u *Updater) getJSON(ctx context.Context, path string, v any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.APIBase+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := u.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v); err != nil {
		return 0, fmt.Errorf("github API: %w", err)
	}
	return resp.StatusCode, nil
}

// install verifies and swaps in rel's asset for this platform. The checksum
// (and signature) are settled before the binary is downloaded, and the
// directory is checked for writability before either.
func (u *Updater) install(ctx context.Context, exe string, before os.FileInfo, rel *ghRelease, requireSig bool, w io.Writer) error {
	name := "ccd-" + u.GOOS + "-" + u.GOARCH
	if u.GOOS == "windows" {
		name += ".exe"
	}
	asset := rel.asset(name)
	if asset == nil {
		return fmt.Errorf("release %s has no %s for this platform", rel.TagName, name)
	}
	if err := preflightWritable(filepath.Dir(exe)); err != nil {
		return err
	}

	sumsAsset := rel.asset(sumsName)
	if sumsAsset == nil {
		return fmt.Errorf("release %s has no %s to verify against (refusing to install)", rel.TagName, sumsName)
	}
	sums, err := u.fetch(ctx, sumsAsset.URL, 1<<20)
	if err != nil {
		return fmt.Errorf("%s: %w", sumsName, err)
	}
	if err := u.verifySignature(ctx, rel, sums, requireSig, w); err != nil {
		return err
	}
	want, err := sumFor(sums, name)
	if err != nil {
		return err
	}
	// GitHub's own digest, when it has one, must agree with the signed file.
	if d := strings.TrimPrefix(asset.Digest, "sha256:"); d != "" && !strings.EqualFold(d, want) {
		return fmt.Errorf("%s: GitHub's digest sha256:%s disagrees with %s (refusing to install)", name, d, sumsName)
	}

	_, _ = fmt.Fprintf(w, "Downloading %s (%.1f MiB)...\n", name, float64(asset.Size)/(1<<20))
	tmp, got, err := u.download(ctx, exe, asset.URL, asset.Size)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) //nolint:errcheck // gone after a successful rename
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s (refusing to install)", name, want, got)
	}
	_, _ = fmt.Fprintf(w, "Checksum OK (sha256:%s)\n", got)
	return commit(u.GOOS, exe, before, tmp)
}

// staleLock is how old a lock file must be before it is taken to be left
// over from a killed update: longer than a whole update may take.
const staleLock = 2 * httpTimeout

// commit swaps tmp in for exe, but only while holding the lock beside exe
// and only if exe is still the file Run found at the start. Two updates run
// at once would otherwise both swap, and the one that resolved its release
// first (perhaps an older one) could land last.
func commit(goos, exe string, before os.FileInfo, tmp string) error {
	unlock, err := lockBeside(exe)
	if err != nil {
		return err
	}
	defer unlock()
	now, err := os.Stat(exe)
	if err != nil || !os.SameFile(before, now) || now.Size() != before.Size() || !now.ModTime().Equal(before.ModTime()) {
		return fmt.Errorf("%s changed while this update ran (another self-update?); nothing replaced, run ccd self-update again", exe)
	}
	return replaceExecutable(goos, exe, tmp)
}

// lockBeside creates the lock file next to exe exclusively and returns its
// release. A lock older than staleLock is removed and taken over once.
func lockBeside(exe string) (unlock func(), err error) {
	path := filepath.Join(filepath.Dir(exe), lockName)
	for attempt := 0; ; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // a fixed name in the executable's directory
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		fi, serr := os.Stat(path)
		if attempt > 0 || serr != nil || time.Since(fi.ModTime()) < staleLock {
			return nil, fmt.Errorf("another ccd self-update is running (lock %s; delete it if none is)", path)
		}
		_ = os.Remove(path)
	}
}

func (r *ghRelease) asset(name string) *ghAsset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

// verifySignature checks the cosign bundle over SHA256SUMS when cosign is
// installed, binding it to this tag's release workflow. Without cosign it
// says what was not checked, or fails when the signature is required.
func (u *Updater) verifySignature(ctx context.Context, rel *ghRelease, sums []byte, require bool, w io.Writer) error {
	cosign, err := u.LookPath("cosign")
	if err != nil {
		if require {
			return errors.New("--require-signature: cosign is not on PATH")
		}
		_, _ = fmt.Fprintln(w, "cosign not found: checking the checksum only, not who signed it.")
		return nil
	}
	bundleAsset := rel.asset(bundleName)
	if bundleAsset == nil {
		return fmt.Errorf("release %s has no %s (refusing to install)", rel.TagName, bundleName)
	}
	bundle, err := u.fetch(ctx, bundleAsset.URL, 1<<20)
	if err != nil {
		return fmt.Errorf("%s: %w", bundleName, err)
	}
	dir, err := os.MkdirTemp("", "ccd-self-update-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // best-effort temp cleanup
	sumsPath, bundlePath := filepath.Join(dir, sumsName), filepath.Join(dir, bundleName)
	if err := os.WriteFile(sumsPath, sums, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		return err
	}
	identity := "https://github.com/" + Repo + "/.github/workflows/release.yml@refs/tags/" + rel.TagName
	//nolint:gosec // cosign from PATH, as the installer uses it; the arguments are not a shell line
	out, err := exec.CommandContext(ctx, cosign, "verify-blob",
		"--bundle", bundlePath,
		"--certificate-identity", identity,
		"--certificate-oidc-issuer", oidcIssuer,
		sumsPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cosign could not verify %s (refusing to install): %v\n%s", sumsName, err, strings.TrimSpace(string(out)))
	}
	_, _ = fmt.Fprintf(w, "%s signature verified (cosign, release.yml@%s)\n", sumsName, rel.TagName)
	return nil
}

// sumFor returns name's hex SHA-256 from a SHA256SUMS file ("<hex>  <name>",
// a leading "*" on the name marking binary mode).
func sumFor(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("%s has no entry for %s (refusing to install)", sumsName, name)
}

// fetch GETs a small release asset into memory, at most limit bytes.
func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// download streams url into a temp file next to exe and returns its path and
// hex SHA-256. The body is cut off past limit (the stated asset size, or
// maxAssetBytes) so a wrong or hostile response cannot fill the disk.
func (u *Updater) download(ctx context.Context, exe, url string, limit int64) (path, sum string, err error) {
	if limit <= 0 {
		limit = maxAssetBytes
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := u.Client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download: %s", resp.Status)
	}

	f, err := os.CreateTemp(filepath.Dir(exe), ".ccd-update-*")
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	n, err := io.Copy(f, io.TeeReader(io.LimitReader(resp.Body, limit+1), h))
	if err == nil && n > limit {
		err = fmt.Errorf("download is larger than the %d bytes the release states", limit)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", "", err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), nil
}

// preflightWritable fails fast, before a ~20 MiB download, when dir does not
// accept a new file.
func preflightWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".ccd-writetest-*")
	if err != nil {
		return &ErrNotWritable{Dir: dir, Err: err}
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

// replaceExecutable renames tmp over exe, keeping exe's permission bits. A
// running .exe cannot be renamed over on Windows, so there the old one is
// moved aside to ".old" first, and moved back if the swap fails.
func replaceExecutable(goos, exe, tmp string) error {
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exe); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return err
	}
	if goos == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			_ = os.Rename(old, exe)
			return err
		}
		return nil
	}
	return os.Rename(tmp, exe)
}
