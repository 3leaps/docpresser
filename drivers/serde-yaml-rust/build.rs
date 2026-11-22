// SPDX-License-Identifier: MIT OR Apache-2.0
//! Exposes the serde_yaml version this driver links as SERDE_YAML_PIN and the
//! libyaml translation it resolves as UNSAFE_LIBYAML_VERSION.
//!
//! Cargo.toml is parsed as TOML and must declare: a `[workspace]` table (the
//! driver is its own workspace root), no `[patch]`, `[replace]` or `[target]`
//! tables, and an exact crates.io runtime dependency
//! `serde_yaml = "=X.Y.Z"` with no package rename. The adjacent Cargo.lock
//! must resolve exactly one crates.io serde_yaml at that version (its build
//! metadata, such as `+deprecated`, is reported from the lock) and
//! exactly one crates.io unsafe-libyaml; a stale, redirected or mismatched
//! lock fails the build instead of producing a wrong report.

use std::env;
use std::fs;
use std::path::PathBuf;

const CRATES_IO: &str = "registry+https://github.com/rust-lang/crates.io-index";

fn main() {
    let dir = PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR"));
    println!("cargo:rerun-if-changed=Cargo.toml");
    println!("cargo:rerun-if-changed=Cargo.lock");
    let manifest = fs::read_to_string(dir.join("Cargo.toml")).expect("read Cargo.toml");
    let pin = manifest_pin(&manifest).unwrap_or_else(|e| panic!("Cargo.toml: {e}"));
    let lock = fs::read_to_string(dir.join("Cargo.lock"))
        .expect("Cargo.lock must exist beside Cargo.toml");
    let locked = locked_version(&lock, "serde_yaml").unwrap_or_else(|e| panic!("Cargo.lock: {e}"));
    // Cargo ignores build metadata in a requirement, so the pin names X.Y.Z and
    // the lock supplies the published version (0.9.34+deprecated).
    let locked_core = locked.split_once('+').map_or(locked.as_str(), |(c, _)| c);
    assert!(
        locked_core == pin,
        "Cargo.lock resolves serde_yaml {locked} but Cargo.toml pins ={pin}"
    );
    let backend =
        locked_version(&lock, "unsafe-libyaml").unwrap_or_else(|e| panic!("Cargo.lock: {e}"));
    println!("cargo:rustc-env=SERDE_YAML_PIN={locked}");
    println!("cargo:rustc-env=UNSAFE_LIBYAML_VERSION={backend}");
}

/// Accepts X.Y.Z with optional `+build` metadata, which the lock carries for
/// serde_yaml `0.9.34+deprecated`.
fn is_version(v: &str) -> bool {
    let (core, build) = match v.split_once('+') {
        Some((c, b)) => (c, Some(b)),
        None => (v, None),
    };
    let parts: Vec<&str> = core.split('.').collect();
    let core_ok = parts.len() == 3
        && parts
            .iter()
            .all(|p| !p.is_empty() && p.bytes().all(|b| b.is_ascii_digit()));
    let build_ok = build.is_none_or(|b| {
        !b.is_empty()
            && b.bytes()
                .all(|c| c.is_ascii_alphanumeric() || c == b'.' || c == b'-')
    });
    core_ok && build_ok
}

/// Returns X.Y.Z from the exact `serde_yaml = "=X.Y.Z"` runtime dependency.
fn manifest_pin(text: &str) -> Result<String, String> {
    let doc: toml::Table = text.parse().map_err(|e| format!("not valid TOML: {e}"))?;
    if !matches!(doc.get("workspace"), Some(toml::Value::Table(_))) {
        return Err("must declare [workspace] so the driver is its own workspace root".into());
    }
    for key in ["patch", "replace", "target"] {
        if doc.contains_key(key) {
            return Err(format!("[{key}] is not supported for this driver"));
        }
    }
    let deps = match doc.get("dependencies") {
        Some(toml::Value::Table(t)) => t,
        _ => return Err("missing [dependencies]".into()),
    };
    for (name, spec) in deps {
        if name != "serde_yaml" {
            if let toml::Value::Table(t) = spec {
                if t.get("package").and_then(toml::Value::as_str) == Some("serde_yaml") {
                    return Err(format!("dependency {name} renames serde_yaml"));
                }
            }
        }
    }
    let req = match deps.get("serde_yaml") {
        Some(toml::Value::String(s)) => s.clone(),
        Some(_) => return Err("serde_yaml must be a plain version string".into()),
        None => return Err("missing the serde_yaml dependency".into()),
    };
    match req.strip_prefix('=') {
        Some(v) if is_version(v) && !v.contains('+') => Ok(v.to_owned()),
        _ => Err(format!(
            "serde_yaml must be pinned exactly as \"=X.Y.Z\", got {req:?}"
        )),
    }
}

/// Returns the version of the single crates.io package `name` in Cargo.lock.
fn locked_version(text: &str, name: &str) -> Result<String, String> {
    let doc: toml::Table = text.parse().map_err(|e| format!("not valid TOML: {e}"))?;
    let packages = match doc.get("package") {
        Some(toml::Value::Array(a)) => a,
        _ => return Err("no [[package]] entries".into()),
    };
    let found: Vec<&toml::Table> = packages
        .iter()
        .filter_map(toml::Value::as_table)
        .filter(|p| p.get("name").and_then(toml::Value::as_str) == Some(name))
        .collect();
    let [pkg] = found.as_slice() else {
        return Err(format!(
            "{} {name} packages; exactly one is required",
            found.len()
        ));
    };
    let source = pkg.get("source").and_then(toml::Value::as_str);
    if source != Some(CRATES_IO) {
        return Err(format!("{name} must come from crates.io, not {source:?}"));
    }
    match pkg.get("version").and_then(toml::Value::as_str) {
        Some(v) if is_version(v) => Ok(v.to_owned()),
        other => Err(format!("{name} version must be X.Y.Z, got {other:?}")),
    }
}
