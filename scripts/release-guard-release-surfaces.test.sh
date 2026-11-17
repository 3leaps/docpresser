#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
# Negative controls for release-guard-release-surfaces.sh.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The source path is resolved relative to this script at runtime.
# shellcheck disable=SC1091
source "${script_dir}/release-guard-release-surfaces.sh"

version='0.1.23'

valid_changelog='# Changelog

## [Unreleased]

## [0.1.23] - 2026-07-26

- A record.

## [0.1.22] - 2026-07-22

- An earlier record.

[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.1.23...HEAD
[0.1.23]: https://github.com/3leaps/docpresser/compare/v0.1.22...v0.1.23
[0.1.22]: https://github.com/3leaps/docpresser/compare/v0.1.21...v0.1.22
'

expect_accepted() {
    local description="$1"
    shift
    if ! validate_release_surfaces "$@" > /dev/null 2>&1; then
        echo "error: positive control was rejected: ${description}" >&2
        exit 1
    fi
    echo "[ok] accepted: ${description}"
}

# expect_rejected <description> <expected-message-fragment> <guard args...>
# The fragment pins which assertion fired. Without it a mutation broken in some
# other dimension would report as a pass.
expect_rejected() {
    local description="$1"
    local expected="$2"
    shift 2
    local output
    if output="$(validate_release_surfaces "$@" 2>&1)"; then
        echo "error: negative control was accepted: ${description}" >&2
        exit 1
    fi
    if [[ "${output}" != *"${expected}"* ]]; then
        echo "error: rejected for the wrong reason: ${description}" >&2
        echo "  expected message to contain: ${expected}" >&2
        echo "  actual: ${output}" >&2
        exit 1
    fi
    echo "[ok] rejected: ${description}"
}

# Case zero: the unmutated fixture must pass, or every rejection below is
# uninformative.
expect_accepted "exact release surfaces" "${version}" "${valid_changelog}"

# A first release links its tag page rather than a compare range.
expect_accepted "first release tag link" "0.1.0" '## [0.1.0] - 2026-11-17

[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/3leaps/docpresser/releases/tag/v0.1.0'

# Lower-case label (Keep a Changelog accepts either).
expect_accepted "lower-case unreleased label" \
    "${version}" "$(printf '%s\n' "${valid_changelog}" | sed 's/^\[Unreleased\]:/[unreleased]:/')"

# mutate <content> <sed-expression>
# Each mutation edits exactly one dimension of an otherwise valid fixture.
mutate() {
    printf '%s\n' "$1" | sed "$2"
}

expect_rejected "changelog has no section for the version" \
    "no section for [0.1.23]" \
    "${version}" "$(mutate "${valid_changelog}" 's/^## \[0\.1\.23\]/## [0.1.23-rc.1]/')"

expect_rejected "a dot in the version is not a wildcard" \
    "no section for [0.1.23]" \
    "${version}" "$(mutate "${valid_changelog}" 's/^## \[0\.1\.23\]/## [0x1x23]/')"

expect_rejected "changelog does not define the version link" \
    "does not define the [0.1.23] link" \
    "${version}" "$(mutate "${valid_changelog}" '/^\[0\.1\.23\]: /d')"

expect_rejected "changelog unreleased link still compares from the previous version" \
    "[Unreleased] link does not compare from v0.1.23" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.23\.\.\.HEAD|v0.1.22...HEAD|')"

expect_rejected "changelog unreleased link absent" \
    "[Unreleased] link does not compare from v0.1.23" \
    "${version}" "$(mutate "${valid_changelog}" '/^\[Unreleased\]: /d')"

# A large changelog whose matching lines come first. A guard that pipes into
# grep -q under pipefail fails here deterministically: grep exits on the early
# match and the still-writing producer dies of SIGPIPE.
large_changelog="$(
    printf '%s\n' "${valid_changelog}" | sed -n '/^\[Unreleased\]/,$p'
    printf '%s\n' "${valid_changelog}" | sed '/^\[/d'
    for _ in $(seq 1 20000); do printf '%s\n' '- Filler entry to keep the producer writing.'; done
)"
expect_accepted "early matches in a large changelog" "${version}" "${large_changelog}"

expect_rejected "empty version" "VERSION is empty" "" "${valid_changelog}"
expect_rejected "malformed version" "is not x.y.z" "0.1" "${valid_changelog}"

echo "[ok] release-surface guard negative controls passed"
