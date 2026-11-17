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
    "must define the [0.1.23] link exactly once (found 0)" \
    "${version}" "$(mutate "${valid_changelog}" '/^\[0\.1\.23\]: /d')"

expect_rejected "changelog unreleased link still compares from the previous version" \
    "[Unreleased] link must be https://github.com/3leaps/docpresser/compare/v0.1.23...HEAD" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.23\.\.\.HEAD|v0.1.22...HEAD|')"

expect_rejected "changelog unreleased link absent" \
    "must define the [Unreleased] link exactly once (found 0)" \
    "${version}" "$(mutate "${valid_changelog}" '/^\[Unreleased\]: /d')"

# Link destinations must name this repository and the right versions, and each
# definition must be unique so a stale first definition cannot shadow a
# correct later one.
expect_rejected "version link points at another release" \
    "[0.1.23] link must be" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[0\.1\.23\]: .*|[0.1.23]: https://github.com/3leaps/docpresser/releases/tag/v9.9.9|')"

expect_rejected "version link points at another repository" \
    "[0.1.23] link must be" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[0\.1\.23\]: https://github.com/3leaps/docpresser/|[0.1.23]: https://github.com/3leaps/other/|')"

expect_rejected "version link compares from a later version" \
    "which is not earlier" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.22\.\.\.v0\.1\.23|v0.1.24...v0.1.23|')"

expect_rejected "version link defined twice" \
    "must define the [0.1.23] link exactly once (found 2)" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\(\[0\.1\.23\]: .*\)|\1\
\1|')"

expect_rejected "stale unreleased link shadows a correct one" \
    "must define the [Unreleased] link exactly once (found 2)" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[Unreleased\]: .*|[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.1.22...HEAD\
[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.1.23...HEAD|')"

expect_rejected "unreleased link points at another repository" \
    "[Unreleased] link must be" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[Unreleased\]: https://github.com/3leaps/docpresser/|[Unreleased]: https://github.com/3leaps/other/|')"

# Definitions are counted the way Markdown resolves them: an indented or
# bracket-padded definition still defines the label, and the first one wins, so
# any second form is ambiguous; definitions inside fenced code create no link.
expect_rejected "indented wrong version definition shadows the real one" \
    "link definition outside the footer" \
    "${version}" "$(printf '   [0.1.23]: https://github.com/3leaps/docpresser/releases/tag/v9.9.9\n%s\n' "${valid_changelog}")"

expect_rejected "bracket-padded wrong version definition shadows the real one" \
    "link definition outside the footer" \
    "${version}" "$(printf '[ 0.1.23 ]: https://github.com/3leaps/docpresser/releases/tag/v9.9.9\n%s\n' "${valid_changelog}")"

expect_rejected "padded unreleased definition shadows the real one" \
    "link definition outside the footer" \
    "${version}" "$(printf '  [ unreleased ]: https://github.com/3leaps/docpresser/compare/v0.1.22...HEAD\n%s\n' "${valid_changelog}")"

expect_rejected "sole version definition is not canonical" \
    "must end with a link-definition footer" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[0\.1\.23\]: |  [0.1.23]: |')"

expect_rejected "version definition only inside a fenced block" \
    "must not contain fenced code blocks" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\(\[0\.1\.23\]: .*\)|```\
\1\
```|')"

expect_rejected "any fenced block, even one holding only an example" \
    "must not contain fenced code blocks" \
    "${version}" "$(printf '~~~\n[0.1.23]: https://example.invalid/\n~~~\n%s\n' "${valid_changelog}")"

# Restricted grammar: each context in which Markdown could hide, shadow or
# neutralize a definition is rejected outright.
W='[0.1.23]: https://github.com/3leaps/docpresser/releases/tag/v9.9.9'
expect_rejected "definition inside a list item" "link definition outside the footer" \
    "${version}" "$(printf -- '- %s\n%s\n' "${W}" "${valid_changelog}")"
expect_rejected "definition inside a blockquote" "link definition outside the footer" \
    "${version}" "$(printf '> %s\n%s\n' "${W}" "${valid_changelog}")"
expect_rejected "multi-line label" "link definition outside the footer" \
    "${version}" "$(printf '[0.1.\n23]: https://github.com/3leaps/docpresser/releases/tag/v9.9.9\n%s\n' "${valid_changelog}")"
expect_rejected "no-break-space padded label" "link definition outside the footer" \
    "${version}" "$(printf '[\302\2400.1.23\302\240]: https://example.invalid/\n%s\n' "${valid_changelog}")"
expect_rejected "em-space padded label" "link definition outside the footer" \
    "${version}" "$(printf '[\342\200\2030.1.23\342\200\203]: https://example.invalid/\n%s\n' "${valid_changelog}")"
expect_rejected "HTML block" "must not contain HTML blocks" \
    "${version}" "$(printf '<div>\n%s\n</div>\n%s\n' "${W}" "${valid_changelog}")"
expect_rejected "four-backtick fence" "must not contain fenced code blocks" \
    "${version}" "$(printf '%s\n````\n' "${valid_changelog}")"
expect_rejected "footer directly after paragraph text" "must end with a link-definition footer" \
    "${version}" "$(mutate "${valid_changelog}" '/^$/d')"
expect_rejected "footer line with a title" "canonical [label]: https://... definition" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\(\[0\.1\.22\]: .*\)|\1 "title"|')"
expect_rejected "footer line with an angle-bracket destination" "canonical [label]: https://... definition" \
    "${version}" "$(mutate "${valid_changelog}" 's|^\[0\.1\.22\]: \(.*\)|[0.1.22]: <\1>|')"
expect_rejected "footer line with a no-break space" "canonical [label]: https://... definition" \
    "${version}" "$(mutate "${valid_changelog}" "s|^\\[0\\.1\\.22\\]: |[0.1.22]:$(printf '\302\240')|")"

# The earlier-version check compares decimal strings, so components beyond the
# shell's integer range cannot wrap to a smaller number.
expect_rejected "earlier version that wraps 64-bit arithmetic" \
    "which is not earlier" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.22\.\.\.v0\.1\.23|v18446744073709551616.0.0...v0.1.23|')"

expect_rejected "earlier version past the signed 64-bit range" \
    "which is not earlier" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.22\.\.\.v0\.1\.23|v9223372036854775808.0.0...v0.1.23|')"

expect_rejected "earlier version with a leading zero component" \
    "[0.1.23] link must be" \
    "${version}" "$(mutate "${valid_changelog}" 's|v0\.1\.22\.\.\.v0\.1\.23|v0.1.022...v0.1.23|')"

# A large changelog whose matching lines come first. A guard that pipes into
# grep -q under pipefail fails here deterministically: grep exits on the early
# match and the still-writing producer dies of SIGPIPE.
large_changelog="$(
    printf '%s\n' "${valid_changelog}" | sed '/^\[/d'
    for _ in $(seq 1 20000); do printf '%s\n' '- Filler entry to keep the producer writing.'; done
    printf '\n'
    printf '%s\n' "${valid_changelog}" | sed -n '/^\[Unreleased\]/,$p'
)"
expect_accepted "early matches in a large changelog" "${version}" "${large_changelog}"

expect_rejected "empty version" "VERSION is empty" "" "${valid_changelog}"
expect_rejected "malformed version" "is not x.y.z" "0.1" "${valid_changelog}"

echo "[ok] release-surface guard negative controls passed"
