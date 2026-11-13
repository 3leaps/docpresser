#!/usr/bin/env bash
# Checks application metadata and template residue without changing source files.
set -euo pipefail

root="${1:-.}"
cd "$root"

if ! command -v rg > /dev/null 2>&1; then
    echo "rg is required for the residue check" >&2
    exit 1
fi
if [[ ! -f .fulmen/app.yaml || ! -f go.mod ]]; then
    echo "application identity or module metadata is missing" >&2
    exit 1
fi

# This list admits only established, full provenance lines. It deliberately does
# not exempt whole provenance files: a new occurrence in README, a hidden ignored
# file, or a binary-like file remains residue. Keep paths relative to the
# repository root.
allowed_provenance_line() {
    local path="$1" text="$2"
    case "$path|$text" in
        'docs/development/foundation.md|[`fulmenhq/forge-microtool-gimlet`](https://github.com/fulmenhq/forge-microtool-gimlet)') return 0 ;;
        'docs/architecture.md|- CLI template: [`fulmenhq/forge-microtool-gimlet`](https://github.com/fulmenhq/forge-microtool-gimlet)') return 0 ;;
        'docs/development/wave-one.md|1. Import `fulmenhq/forge-microtool-gimlet` (`v0.2.0`,') return 0 ;;
        'docs/development/wave-one.md|   (retaining Gimlet MIT provenance), plus `docs/development/foundation.md`') return 0 ;;
        'NOTICE|https://github.com/fulmenhq/forge-microtool-gimlet (Copyright (c) 2025 3 Leaps,') return 0 ;;
        'scripts/license-check.py|    "https://github.com/fulmenhq/forge-microtool-gimlet",') return 0 ;;
    esac
    return 1
}

template_pattern='gimlet|gimlet_|forge-microtool-gimlet|forge-workhorse-groningen|groningen_|queuepresser|QUEUEPRESSER_'
matches="$(mktemp "${TMPDIR:-/tmp}/docpresser-cdrl-rg.XXXXXX")"
trap 'rm -f "$matches"' EXIT

if rg -n --no-ignore --hidden -a --ignore-case \
    --glob '!.git/**' \
    --glob '!scripts/cdrl-verify.sh' \
    --glob '!bin/**' \
    --glob '!dist/**' \
    --glob '!vendor/**' \
    --glob '!coverage/**' \
    --glob '!.ruff_cache/**' \
    --glob '!node_modules/**' \
    "$template_pattern" . > "$matches"; then
    rg_status=0
else
    rg_status=$?
fi
if ((rg_status != 0 && rg_status != 1)); then
    echo "rg failed while checking template residue (status $rg_status)" >&2
    exit 1
fi

residue=0
while IFS= read -r match; do
    if [[ "$match" =~ ^\./([^:]+):([0-9]+):(.*)$ ]]; then
        path="${BASH_REMATCH[1]}"
        line="${BASH_REMATCH[2]}"
        text="${BASH_REMATCH[3]}"
        if allowed_provenance_line "$path" "$text"; then
            continue
        fi
        printf '%s:%s\n' "$path" "$line" >&2
        residue=1
        continue
    fi
    echo "template residue detected in an unparseable match record" >&2
    residue=1
done < "$matches"

if ((residue)); then
    echo "template residue detected" >&2
    exit 1
fi
