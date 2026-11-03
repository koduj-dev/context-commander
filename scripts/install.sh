#!/bin/sh
# install.sh — download a ccd release binary, verify it, and install it.
#
#   curl -fsSLO https://raw.githubusercontent.com/koduj-dev/context-commander/main/scripts/install.sh
#   sh install.sh [--version vX.Y.Z] [--pre] [--bin-dir DIR] [--require-signature]
#
# What it installs is checked before anything is written to the target: the
# binary must match its line in the release's SHA256SUMS, and when cosign is
# on PATH (or --require-signature is given) SHA256SUMS itself must carry the
# keyless signature release.yml made for exactly that tag. Without cosign the
# checksum only proves the download is intact, not who published it; the
# script says so.
#
# Which release: the newest stable one (GitHub's "Latest"). While the project
# has no stable release yet, the newest pre-release instead, so the default
# always finds something. --pre takes the newest release, pre-releases
# included; --version pins one. `ccd self-update` resolves the same way.
#
# Where: --bin-dir, else /usr/local/bin when it is writable (root, or a
# Homebrew-style user-owned /usr/local), else ~/.local/bin. It never calls
# sudo itself: run it with sudo for a system-wide install. Manual pages go to
# ../share/man/man1 next to the bin directory when the release ships them
# (ccd-man.tar.gz); --no-man skips them.
#
# Linux and macOS, amd64 and arm64. Needs curl, tar and one of sha256sum,
# shasum or openssl. Windows: take ccd-windows-*.exe from the Releases page.
#
# CCD_GITHUB_API and CCD_GITHUB_URL point it at another API / download host
# (a mirror, or the tests in scripts/install_test.go).
#
# Everything runs from main() at the very end, so a download of this script
# cut short cannot execute half of it.
set -eu

REPO="koduj-dev/context-commander"
API="${CCD_GITHUB_API:-https://api.github.com}"
DL="${CCD_GITHUB_URL:-https://github.com}"
OIDC_ISSUER="https://token.actions.githubusercontent.com"
# HTTPS only, redirects included, unless a test or mirror overrides the hosts.
PROTO="=https"
if [ -n "${CCD_GITHUB_API:-}${CCD_GITHUB_URL:-}" ]; then PROTO="=https,http"; fi

say() { printf '%s\n' "$*"; }
warn() { printf 'install.sh: %s\n' "$*" >&2; }
die() {
	warn "$*"
	exit 1
}

usage() {
	cat <<'EOF'
Usage: install.sh [options]

  --version TAG        install this release (e.g. v0.1.0-beta.1)
  --pre                newest release, pre-releases included
  --bin-dir DIR        install ccd into DIR
  --man-dir DIR        install the manual pages into DIR
  --no-man             do not install the manual pages
  --require-signature  fail unless cosign verifies SHA256SUMS
  -h, --help           this text
EOF
}

# fetch URL FILE — download URL to FILE; fails on any HTTP error.
fetch() {
	curl -fsSL --proto "$PROTO" --proto-redir "$PROTO" --retry 3 -o "$2" "$1"
}

# json_tag FILE — the first non-draft release's tag_name in a GitHub API
# response (one release object, or a list of them, newest first). Plain
# text processing so the script needs no jq: GitHub puts tag_name before
# draft in a release object, and a string value cannot produce a match
# because its quotes are escaped.
json_tag() {
	tr ',' '\n' <"$1" | awk '
		/^[[:space:]{[]*"tag_name"[[:space:]]*:/ {
			v = $0; sub(/^[^:]*:[[:space:]]*"/, "", v); sub(/".*$/, "", v); tag = v; next
		}
		/^[[:space:]{[]*"draft"[[:space:]]*:[[:space:]]*false/ { if (tag != "") { print tag; exit } }
		/^[[:space:]{[]*"draft"[[:space:]]*:[[:space:]]*true/ { tag = "" }
	'
}

# resolve_tag PRE TMP — the tag to install (see the header for the rule).
resolve_tag() {
	if [ "$1" = 0 ]; then
		code="$(curl -sSL --proto "$PROTO" --proto-redir "$PROTO" --retry 3 -o "$2/latest.json" -w '%{http_code}' \
			-H 'Accept: application/vnd.github+json' "$API/repos/$REPO/releases/latest")" ||
			die "cannot reach $API"
		case "$code" in
		200)
			json_tag "$2/latest.json"
			return
			;;
		404) warn "no stable release yet, using the newest pre-release" ;;
		*) die "GitHub API answered HTTP $code for the latest release" ;;
		esac
	fi
	fetch "$API/repos/$REPO/releases?per_page=20" "$2/releases.json" ||
		die "cannot list the releases of $REPO"
	json_tag "$2/releases.json"
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		die "need sha256sum, shasum or openssl to verify the download"
	fi
}

# verify SUMS FILE NAME — FILE must hash to NAME's line in SUMS.
verify() {
	want="$(awk -v f="$3" '{n = $2; sub(/^\*/, "", n)} n == f {print $1; exit}' "$1")"
	[ -n "$want" ] || die "SHA256SUMS has no entry for $3"
	got="$(sha256 "$2")"
	[ "$want" = "$got" ] || die "checksum mismatch for $3: expected $want, got $got (refusing to install)"
}

# default_bin_dir — /usr/local/bin if this user can write it, else ~/.local/bin.
default_bin_dir() {
	if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		echo /usr/local/bin
	elif [ "$(id -u)" = 0 ]; then
		echo /usr/local/bin
	else
		[ -n "${HOME:-}" ] || die "HOME is not set; pass --bin-dir"
		echo "$HOME/.local/bin"
	fi
}

main() {
	tag="" pre=0 bin_dir="" man_dir="" no_man=0 require_sig=0
	while [ $# -gt 0 ]; do
		case "$1" in
		--version)
			[ $# -ge 2 ] || die "--version needs a value"
			tag="$2"
			shift
			;;
		--version=*) tag="${1#*=}" ;;
		--pre | --prerelease) pre=1 ;;
		--bin-dir)
			[ $# -ge 2 ] || die "--bin-dir needs a value"
			bin_dir="$2"
			shift
			;;
		--bin-dir=*) bin_dir="${1#*=}" ;;
		--man-dir)
			[ $# -ge 2 ] || die "--man-dir needs a value"
			man_dir="$2"
			shift
			;;
		--man-dir=*) man_dir="${1#*=}" ;;
		--no-man) no_man=1 ;;
		--require-signature) require_sig=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			usage >&2
			die "unknown option: $1"
			;;
		esac
		shift
	done

	command -v curl >/dev/null 2>&1 || die "curl is required"
	have_cosign=0
	if command -v cosign >/dev/null 2>&1; then
		have_cosign=1
	elif [ "$require_sig" = 1 ]; then
		die "--require-signature: cosign is not on PATH"
	fi

	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "unsupported OS $(uname -s); take a binary from $DL/$REPO/releases" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "unsupported architecture $(uname -m)" ;;
	esac
	asset="ccd-$os-$arch"

	tmp="$(mktemp -d 2>/dev/null || mktemp -d -t ccd-install)"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' HUP INT TERM

	if [ -z "$tag" ]; then
		tag="$(resolve_tag "$pre" "$tmp")"
		[ -n "$tag" ] || die "found no release of $REPO"
	fi
	case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	# The tag goes into URLs and the signature identity: semver only.
	printf '%s\n' "$tag" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
		die "not a release tag: $tag"
	base="$DL/$REPO/releases/download/$tag"
	say "==> ccd $tag ($os/$arch)"

	fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "cannot download SHA256SUMS for $tag (does the release exist?)"

	if [ "$have_cosign" = 1 ]; then
		fetch "$base/SHA256SUMS.bundle" "$tmp/SHA256SUMS.bundle" || die "release $tag has no SHA256SUMS.bundle"
		cosign verify-blob --bundle "$tmp/SHA256SUMS.bundle" \
			--certificate-identity "https://github.com/$REPO/.github/workflows/release.yml@refs/tags/$tag" \
			--certificate-oidc-issuer "$OIDC_ISSUER" "$tmp/SHA256SUMS" >/dev/null 2>&1 ||
			die "cosign could not verify the signature on SHA256SUMS (refusing to install)"
		say "==> SHA256SUMS signature verified (cosign, release.yml@$tag)"
	else
		warn "cosign not found: checking checksums only, not who signed them"
	fi

	fetch "$base/$asset" "$tmp/$asset" || die "cannot download $asset"
	verify "$tmp/SHA256SUMS" "$tmp/$asset" "$asset"
	say "==> $asset checksum OK"

	[ -n "$bin_dir" ] || bin_dir="$(default_bin_dir)"
	mkdir -p "$bin_dir" || die "cannot create $bin_dir"
	[ -w "$bin_dir" ] || die "$bin_dir is not writable; run with sudo or pass --bin-dir"
	# Copy into the target directory first, then rename: a running ccd, or
	# one started meanwhile, sees the old binary or the new one, never half.
	cp "$tmp/$asset" "$bin_dir/.ccd.install.$$"
	chmod 0755 "$bin_dir/.ccd.install.$$"
	mv -f "$bin_dir/.ccd.install.$$" "$bin_dir/ccd"
	say "==> installed $bin_dir/ccd"

	if [ "$no_man" = 0 ]; then
		if grep -Eq '[[:space:]]\*?ccd-man\.tar\.gz$' "$tmp/SHA256SUMS"; then
			if [ -z "$man_dir" ] && [ "$(basename "$bin_dir")" = bin ]; then
				man_dir="$(dirname "$bin_dir")/share/man/man1"
			fi
			if [ -n "$man_dir" ]; then
				fetch "$base/ccd-man.tar.gz" "$tmp/ccd-man.tar.gz" || die "cannot download ccd-man.tar.gz"
				verify "$tmp/SHA256SUMS" "$tmp/ccd-man.tar.gz" ccd-man.tar.gz
				mkdir -p "$tmp/man" "$man_dir" || die "cannot create $man_dir"
				tar -xzf "$tmp/ccd-man.tar.gz" -C "$tmp/man"
				n=0
				for page in "$tmp"/man/ccd*.1.gz; do
					[ -f "$page" ] || continue
					cp "$page" "$man_dir/" && chmod 0644 "$man_dir/$(basename "$page")"
					n=$((n + 1))
				done
				say "==> $n manual pages in $man_dir (man ccd)"
			else
				warn "not installing manual pages: pass --man-dir for a bin directory not named bin"
			fi
		fi
	fi

	"$bin_dir/ccd" version >/dev/null 2>&1 || warn "$bin_dir/ccd does not run on this machine"
	found="$(command -v ccd 2>/dev/null || true)"
	if [ -z "$found" ]; then
		warn "$bin_dir is not on PATH; add it, e.g.: export PATH=\"$bin_dir:\$PATH\""
	elif [ "$found" != "$bin_dir/ccd" ]; then
		warn "ccd on PATH is $found, not the one just installed; that one runs first"
	fi
	if "$bin_dir/ccd" self-update --help >/dev/null 2>&1; then
		say "Done. Update later with: ccd self-update"
	else
		say "Done. Update later by running this script again."
	fi
}

main "$@"
