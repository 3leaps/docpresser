// SPDX-License-Identifier: MIT OR Apache-2.0
//! Exposes the exact `yamlprims` pin from Cargo.toml as YAMLPRIMS_PIN so the
//! driver reports the version it links. The pin must be exact (`=X.Y.Z`); any
//! other form fails the build rather than reporting an unverified version.

use std::fs;

fn main() {
    println!("cargo:rerun-if-changed=Cargo.toml");
    let manifest = fs::read_to_string("Cargo.toml").expect("read Cargo.toml");
    let mut pins = manifest.lines().filter_map(|line| {
        let rest = line.trim().strip_prefix("yamlprims")?.trim_start();
        let value = rest.strip_prefix('=')?.trim();
        value
            .strip_prefix("\"=")?
            .strip_suffix('"')
            .map(str::to_owned)
    });
    let pin = pins
        .next()
        .expect("Cargo.toml must pin yamlprims exactly, as yamlprims = \"=X.Y.Z\"");
    assert!(
        pins.next().is_none(),
        "Cargo.toml must pin yamlprims exactly once"
    );
    assert!(
        !pin.is_empty()
            && pin.split('.').count() == 3
            && pin
                .split('.')
                .all(|part| !part.is_empty() && part.bytes().all(|b| b.is_ascii_digit())),
        "the yamlprims pin must be X.Y.Z, got {pin:?}"
    );
    println!("cargo:rustc-env=YAMLPRIMS_PIN={pin}");
}
