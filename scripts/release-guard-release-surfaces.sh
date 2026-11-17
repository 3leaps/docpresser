#!/usr/bin/env bash
# SPDX-License-Identifier: MIT OR Apache-2.0
# release-guard-release-surfaces.sh - Verify CHANGELOG.md matches VERSION
#
# The changelog is maintained by hand and can fall behind VERSION without any
# other check failing, so the repository can present a stale release surface
# from a green tree. This guard asserts they moved together.
#
# Checks, against the version in VERSION:
# - CHANGELOG.md carries a "## [<version>]" section
# - CHANGELOG.md defines the "[<version>]: " link exactly once, pointing at this
#   repository: compare/v<earlier>...v<version>, or releases/tag/v<version> for
#   the first release
# - CHANGELOG.md defines [Unreleased] exactly once, as
#   compare/v<version>...HEAD in this repository
#
# Usage: ./scripts/release-guard-release-surfaces.sh

set -euo pipefail

REPO_URL="https://github.com/3leaps/docpresser"

# version_lt <a> <b>: true when x.y.z version a sorts strictly before b. Each
# component is compared as a decimal string (leading zeros dropped, then length,
# then digits), so arbitrarily long components cannot overflow shell arithmetic.
version_lt() {
    local IFS=.
    local -a a=($1) b=($2)
    local i x y
    for i in 0 1 2; do
        x="${a[i]#"${a[i]%%[!0]*}"}"
        y="${b[i]#"${b[i]%%[!0]*}"}"
        if [ "${#x}" -ne "${#y}" ]; then
            [ "${#x}" -lt "${#y}" ]
            return
        fi
        if [ "${x}" != "${y}" ]; then
            [[ "${x}" < "${y}" ]]
            return
        fi
    done
    return 1
}

# effective_markdown <text>: the text with fenced code blocks removed, so link
# definitions shown as examples inside fences are never counted.
effective_markdown() {
    awk '
        /^ {0,3}(```|~~~)/ { fenced = !fenced; next }
        !fenced { print }
    ' <<< "$1"
}

# definition_lines <text> <label-regex>: every line that CommonMark could treat
# as a definition of the label: up to any indentation, padding inside the
# brackets, either case. Callers require exactly one, in canonical form.
definition_lines() {
    grep -iE "^[[:space:]]*\[[[:space:]]*$2[[:space:]]*\]:" <<< "$1" || true
}

# validate_release_surfaces <version> <changelog> [repo_url]
validate_release_surfaces() {
    local version="$1"
    local changelog="$2"
    local repo="${3:-$REPO_URL}"

    if [ -z "${version}" ]; then
        echo "error: VERSION is empty" >&2
        return 1
    fi
    if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "error: VERSION ${version} is not x.y.z" >&2
        return 1
    fi
    local escaped="${version//./\\.}"

    # Match from a here-string, never a pipe. grep -q exits on the first hit
    # and closes the pipe; under pipefail the producer's SIGPIPE then turns a
    # match into a false failure once CHANGELOG is large enough.
    if ! grep -qE "^## \[${escaped}\]( |$)" <<< "${changelog}"; then
        echo "error: CHANGELOG.md has no section for [${version}]" >&2
        return 1
    fi

    local effective defs count line earlier
    effective="$(effective_markdown "${changelog}")"

    defs="$(definition_lines "${effective}" "${escaped}")"
    count="$(grep -c . <<< "${defs}" || true)"
    if [ "${count}" != 1 ]; then
        echo "error: CHANGELOG.md must define the [${version}] link exactly once (found ${count})" >&2
        return 1
    fi
    line="${defs}"
    if [ "${line}" != "[${version}]: ${repo}/releases/tag/v${version}" ]; then
        if [[ "${line}" =~ ^\[${escaped}\]:\ (.*)/compare/v((0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))\.\.\.v${escaped}$ ]] &&
            [ "${BASH_REMATCH[1]}" = "${repo}" ]; then
            earlier="${BASH_REMATCH[2]}"
            if ! version_lt "${earlier}" "${version}"; then
                echo "error: CHANGELOG.md [${version}] link compares from v${earlier}, which is not earlier" >&2
                return 1
            fi
        else
            echo "error: CHANGELOG.md [${version}] link must be ${repo}/compare/v<earlier>...v${version} or ${repo}/releases/tag/v${version}" >&2
            return 1
        fi
    fi

    defs="$(definition_lines "${effective}" "unreleased")"
    count="$(grep -c . <<< "${defs}" || true)"
    if [ "${count}" != 1 ]; then
        echo "error: CHANGELOG.md must define the [Unreleased] link exactly once (found ${count})" >&2
        return 1
    fi
    if [[ ! "${defs}" =~ ^\[[Uu]nreleased\]:\ (.*)$ ]] ||
        [ "${BASH_REMATCH[1]}" != "${repo}/compare/v${version}...HEAD" ]; then
        echo "error: CHANGELOG.md [Unreleased] link must be ${repo}/compare/v${version}...HEAD" >&2
        return 1
    fi

    return 0
}

main() {
    local script_dir repo_root version changelog

    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    repo_root="$(cd "${script_dir}/.." && pwd)"

    if [ ! -f "${repo_root}/VERSION" ]; then
        echo "error: VERSION file not found" >&2
        exit 1
    fi
    version="$(tr -d '[:space:]' < "${repo_root}/VERSION")"

    if [ ! -f "${repo_root}/CHANGELOG.md" ]; then
        echo "error: CHANGELOG.md not found" >&2
        exit 1
    fi
    changelog="$(cat "${repo_root}/CHANGELOG.md")"

    if ! validate_release_surfaces "${version}" "${changelog}"; then
        echo "hint: run make sync-changelog-links, then see RELEASE_CHECKLIST.md" >&2
        exit 1
    fi

    echo "[ok] release surfaces match VERSION ${version}"
}

# Only run main when executed directly, so the negative controls can source this
# file and drive validate_release_surfaces with mutated content.
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    main "$@"
fi
