#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd -P)"
cd "$root"
suites="$root/config/release/tooling-suites.txt"
[[ -f "$suites" && ! -L "$suites" ]] || {
    echo 'error: missing config/release/tooling-suites.txt' >&2
    exit 1
}
expected="$(LC_ALL=C sort "$suites")"
actual="$(find "$root/scripts" -maxdepth 1 -name '*.test.sh' -print | sed 's|.*/||; s|\.test\.sh$||' | LC_ALL=C sort)"
[[ "$expected" == "$actual" ]] || {
    echo 'error: scripts/*.test.sh does not match config/release/tooling-suites.txt' >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >&2 || true
    exit 1
}
# Suites use throwaway keys and fixtures only. Clear the maintainer's release
# environment so a real tag, keyring, key id or catalog can never leak into a
# fixture; only the designated checker path is passed through. Every GIT_*
# variable is cleared too (repository redirects such as GIT_DIR, and injected
# configuration such as GIT_CONFIG_COUNT/GIT_CONFIG_PARAMETERS), system git
# configuration is ignored, and global configuration is a sealed file holding
# only a fixture identity with signing off, so no suite can reach an operator
# repository or signing setup.
scrub=()
while IFS= read -r name; do
    [[ "$name" == DOCPRESSER_DECERNOR_BIN ]] || scrub+=(-u "$name")
done < <(compgen -e | grep -E '^(DOCPRESSER_|LIMENSAFE_|GNUPGHOME$|GIT_)' || true)
gitconfig="$(mktemp "${TMPDIR:-/tmp}/release-tooling-gitconfig.XXXXXX")"
trap 'rm -f "$gitconfig"' EXIT
cat > "$gitconfig" << 'GITCONFIG'
[user]
	name = Release Tooling Fixture
	email = fixture@example.invalid
[commit]
	gpgsign = false
[tag]
	gpgsign = false
[init]
	defaultBranch = main
GITCONFIG
while IFS= read -r suite; do
    [[ -n "$suite" ]] || continue
    env ${scrub[@]+"${scrub[@]}"} GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$gitconfig" \
        "$root/scripts/$suite.test.sh"
done < "$suites"
echo "[ok] all $(wc -l < "$suites" | tr -d ' ') release tooling suites passed"
