#!/usr/bin/env bash
# Keep the release-bound facts of the docs in step with the latest GitHub
# release and go.mod: version strings, the landing banner, the Go minor
# version and the number of direct dependencies.
#
#   scripts/landing-sync.sh            rewrite the files for the latest release
#   scripts/landing-sync.sh 0.7.0      rewrite them for a given version
#   scripts/landing-sync.sh --check    exit 1 when the files are out of date
#
# The Pages workflow runs it before deploying, so the published page follows
# every release even when the committed files lag behind. Needs gh (or
# GH_TOKEN) to read the release; perl does the rewriting on macOS and Linux.
set -euo pipefail
cd "$(dirname "$0")/.."

check=0
version=""
for arg in "$@"; do
  case "$arg" in
    --check) check=1 ;;
    *) version="${arg#v}" ;;
  esac
done
if [ -z "$version" ]; then
  version=$(gh release view --json tagName -q .tagName)
  version=${version#v}
fi

go_minor=$(awk '/^go /{split($2, a, "."); print a[1] "." a[2]}' go.mod)
deps=$(awk '/^require \(/{f=1; next} /^\)/{f=0} f && NF && !/indirect/' go.mod | wc -l | tr -d ' ')
words=(zero one two three four five six seven eight nine ten)
deps_word=${words[$deps]:-$deps}

# Banner headline: the first feature of the release notes, without the commit
# hash, the conventional prefix and the author; an empty headline keeps the
# banner to "vX.Y.Z is out".
headline=$(gh release view "v$version" --json body -q .body 2>/dev/null \
  | awk '/^### Features/{f=1; next} /^###/{f=0} f && /^\* /{print; exit}' \
  | sed -E 's/^\* [0-9a-f]+: feat(\([^)]*\))?: //; s/ \(@[^)]*\)$//' || true)

export version go_minor deps_word headline
rewrite() {
  perl -pi -e '
    my ($v, $go, $deps, $h) = @ENV{qw(version go_minor deps_word headline)};
    (my $hh = $h) =~ s/&/&amp;/g; $hh =~ s/</&lt;/g; $hh =~ s/>/&gt;/g;
    my $banner = length $h ? "v$v is out: " . ucfirst($hh) . "." : "v$v is out.";
    s{v\d+\.\d+\.\d+}{v$v}g;
    s{_\d+\.\d+\.\d+_}{_${v}_}g;
    s{(VERSION=|arazzo-maestro:)\d+\.\d+\.\d+}{$1$v}g;
    s{<span class="rel">.*?</span>}{<span class="rel">$banner</span>};
    s{Go \d+\.\d+( or newer|\+)}{Go $go$1}g;
    s{go-version: \x27\d+\.\d+\x27}{go-version: \x27$go\x27}g;
    s{\b(\w+)( direct dependencies)}{my ($w, $rest) = ($1, $2); ($w =~ /^[A-Z]/ ? ucfirst $deps : $deps) . $rest}ge;
  ' "$1"
}

status=0
for f in docs/index.html README.md docs/RECIPES.md; do
  if [ "$check" = 1 ]; then
    tmp=$(mktemp)
    cp "$f" "$tmp"
    rewrite "$tmp"
    if ! diff -u "$f" "$tmp" > /dev/null; then
      echo "$f is out of date for v$version:"
      diff -u "$f" "$tmp" | grep -E '^[-+][^-+]' || true
      status=1
    fi
    rm -f "$tmp"
  else
    rewrite "$f"
  fi
done
[ "$check" = 1 ] && [ "$status" = 0 ] && echo "docs in step with v$version, Go $go_minor, $deps_word direct dependencies"
exit $status
