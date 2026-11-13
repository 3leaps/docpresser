#!/usr/bin/env bash
# Exercises residue detection in an isolated temporary tree.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/docpresser-cdrl.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/.fulmen" "$tmp/scripts" "$tmp/bin"
printf 'module example.invalid/docpresser\n' > "$tmp/go.mod"
printf 'binary_name: docpresser\n' > "$tmp/.fulmen/app.yaml"
printf '.env\n' > "$tmp/.gitignore"
cp "$repo_root/scripts/cdrl-verify.sh" "$tmp/scripts/cdrl-verify.sh"
chmod +x "$tmp/scripts/cdrl-verify.sh"

if ! "$tmp/scripts/cdrl-verify.sh" "$tmp" > "$tmp/output" 2>&1; then
    cat "$tmp/output" >&2
    echo "clean temporary tree was rejected" >&2
    exit 1
fi

expect_residue() {
    local name="$1"
    if "$tmp/scripts/cdrl-verify.sh" "$tmp" > "$tmp/output" 2>&1; then
        echo "$name residue was accepted" >&2
        exit 1
    fi
    grep -qx 'template residue detected' "$tmp/output"
}

printf '%s\n' "GI""MLET_ secret-value-must-not-appear" > "$tmp/README.md"
expect_residue 'README'
grep -qx 'README.md:1' "$tmp/output"
if grep -q 'secret-value-must-not-appear' "$tmp/output"; then
    echo "residue output leaked matched text" >&2
    exit 1
fi
rm "$tmp/README.md"

printf '%s\n' "GI""MLET_" > "$tmp/.env"
expect_residue 'hidden ignored'
rm "$tmp/.env"

printf 'prefix\0GI''MLET_suffix\n' > "$tmp/residue.bin"
expect_residue 'binary-like'
rm "$tmp/residue.bin"

printf 'prefix\0GI''MLET_suffix\n' > "$tmp/bin/docpresser"
if ! "$tmp/scripts/cdrl-verify.sh" "$tmp" > "$tmp/output" 2>&1; then
    cat "$tmp/output" >&2
    echo "documented generated-output exclusion was rejected" >&2
    exit 1
fi

mkdir "$tmp/fake-bin"
cat > "$tmp/fake-bin/rg" << 'EOF'
#!/usr/bin/env bash
exit 2
EOF
chmod +x "$tmp/fake-bin/rg"
if PATH="$tmp/fake-bin:$PATH" "$tmp/scripts/cdrl-verify.sh" "$tmp" > "$tmp/output" 2>&1; then
    echo "rg failure was accepted" >&2
    exit 1
fi
grep -qx 'rg failed while checking template residue (status 2)' "$tmp/output"
