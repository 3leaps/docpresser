// SPDX-License-Identifier: MIT OR Apache-2.0
//! Exposes the yamlprims version this driver actually links as YAMLPRIMS_PIN.
//!
//! The version comes from Cargo.lock, which Cargo writes during dependency
//! resolution before build scripts run, so manifest entries that are not the
//! resolved dependency (package metadata, comments, dev-dependencies) cannot
//! supply it. The build fails unless the lock holds exactly one yamlprims
//! package, from the crates.io registry, with an X.Y.Z version. The package
//! is its own workspace root, so that adjacent lock is the one Cargo uses.

use std::env;
use std::fs;
use std::path::PathBuf;

const CRATES_IO: &str = "registry+https://github.com/rust-lang/crates.io-index";

fn main() {
    let dir = PathBuf::from(env::var("CARGO_MANIFEST_DIR").expect("CARGO_MANIFEST_DIR"));
    let lock_path = dir.join("Cargo.lock");
    println!("cargo:rerun-if-changed={}", lock_path.display());
    println!("cargo:rerun-if-changed=Cargo.toml");
    // Cargo uses the adjacent lock only when this package is its own workspace
    // root; inside another workspace the root's lock would be authoritative.
    let manifest = fs::read_to_string(dir.join("Cargo.toml")).expect("read Cargo.toml");
    assert!(
        manifest.lines().any(|line| line.trim() == "[workspace]"),
        "Cargo.toml must declare this package its own workspace root ([workspace])"
    );
    let lock = fs::read_to_string(&lock_path).expect("Cargo.lock must exist beside Cargo.toml");
    let version = resolved_version(&lock, "yamlprims").unwrap_or_else(|e| panic!("{e}"));
    println!("cargo:rustc-env=YAMLPRIMS_PIN={version}");
}

/// Returns the version of the single crates.io package `name` in a Cargo.lock.
fn resolved_version(lock: &str, name: &str) -> Result<String, String> {
    let mut found = Vec::new();
    for block in lock.split("[[package]]").skip(1) {
        let field = |key: &str| {
            block.lines().find_map(|line| {
                let value = line.strip_prefix(key)?.strip_prefix(" = \"")?;
                value.strip_suffix('"').map(str::to_owned)
            })
        };
        if field("name").as_deref() == Some(name) {
            found.push((field("version"), field("source")));
        }
    }
    match found.as_slice() {
        [(Some(version), Some(source))] => {
            if source != CRATES_IO {
                return Err(format!("{name} must come from crates.io, not {source}"));
            }
            let parts: Vec<&str> = version.split('.').collect();
            if parts.len() != 3
                || parts
                    .iter()
                    .any(|p| p.is_empty() || !p.bytes().all(|b| b.is_ascii_digit()))
            {
                return Err(format!("{name} version must be X.Y.Z, got {version:?}"));
            }
            Ok(version.clone())
        }
        [] => Err(format!("Cargo.lock has no {name} package")),
        [_] => Err(format!(
            "Cargo.lock entry for {name} lacks a version or source"
        )),
        _ => Err(format!(
            "Cargo.lock has {} {name} packages; exactly one is required",
            found.len()
        )),
    }
}
