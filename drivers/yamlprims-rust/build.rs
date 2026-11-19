// SPDX-License-Identifier: MIT OR Apache-2.0
//! Exposes the yamlprims version this driver links as YAMLPRIMS_PIN.
//!
//! Cargo.toml is parsed as TOML and must declare: a `[workspace]` table (the
//! driver is its own workspace root), no `[patch]`, `[replace]` or `[target]`
//! tables, and an exact crates.io runtime dependency `yamlprims = "=X.Y.Z"`
//! with no package rename. An exact requirement lets Cargo link only that
//! version. The adjacent Cargo.lock must also resolve exactly one crates.io
//! yamlprims at that same version; a stale, redirected or mismatched lock fails
//! the build instead of producing a wrong report.

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
    let locked = locked_version(&lock).unwrap_or_else(|e| panic!("Cargo.lock: {e}"));
    assert!(
        locked == pin,
        "Cargo.lock resolves yamlprims {locked} but Cargo.toml pins ={pin}"
    );
    println!("cargo:rustc-env=YAMLPRIMS_PIN={pin}");
}

fn is_version(v: &str) -> bool {
    let parts: Vec<&str> = v.split('.').collect();
    parts.len() == 3
        && parts
            .iter()
            .all(|p| !p.is_empty() && p.bytes().all(|b| b.is_ascii_digit()))
}

/// Returns X.Y.Z from the exact `yamlprims = "=X.Y.Z"` runtime dependency.
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
        if name != "yamlprims" {
            if let toml::Value::Table(t) = spec {
                if t.get("package").and_then(toml::Value::as_str) == Some("yamlprims") {
                    return Err(format!("dependency {name} renames yamlprims"));
                }
            }
        }
    }
    let req = match deps.get("yamlprims") {
        Some(toml::Value::String(s)) => s.clone(),
        Some(_) => return Err("yamlprims must be a plain version string".into()),
        None => return Err("missing the yamlprims dependency".into()),
    };
    match req.strip_prefix('=') {
        Some(v) if is_version(v) => Ok(v.to_owned()),
        _ => Err(format!(
            "yamlprims must be pinned exactly as \"=X.Y.Z\", got {req:?}"
        )),
    }
}

/// Returns the version of the single crates.io yamlprims package in Cargo.lock.
fn locked_version(text: &str) -> Result<String, String> {
    let doc: toml::Table = text.parse().map_err(|e| format!("not valid TOML: {e}"))?;
    let packages = match doc.get("package") {
        Some(toml::Value::Array(a)) => a,
        _ => return Err("no [[package]] entries".into()),
    };
    let found: Vec<&toml::Table> = packages
        .iter()
        .filter_map(toml::Value::as_table)
        .filter(|p| p.get("name").and_then(toml::Value::as_str) == Some("yamlprims"))
        .collect();
    let [pkg] = found.as_slice() else {
        return Err(format!(
            "{} yamlprims packages; exactly one is required",
            found.len()
        ));
    };
    let source = pkg.get("source").and_then(toml::Value::as_str);
    if source != Some(CRATES_IO) {
        return Err(format!(
            "yamlprims must come from crates.io, not {source:?}"
        ));
    }
    match pkg.get("version").and_then(toml::Value::as_str) {
        Some(v) if is_version(v) => Ok(v.to_owned()),
        other => Err(format!("yamlprims version must be X.Y.Z, got {other:?}")),
    }
}
