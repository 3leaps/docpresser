# Changelog

## [Unreleased]

- Breaking (run-manifest.json): `docpresser run` now calls each driver with `--docpresser-handshake` before any case and records what it reports. Each `subjects[]` entry carries `driver_subject` (the subject name the driver reports), `subject_version`, `language`, `engine`, `formats`, and `operations`; the `version` field is removed. The manifest also records `docpresser_version`, the docpresser build that wrote it. A failed or invalid handshake stops the run before any evidence is written. In manifests written by 0.1.0, `subjects[].version` is a fixed placeholder and does not identify the subject's version.
- A case response whose `subject_version` differs from the handshake is recorded as `protocol_error` with `subject_version_mismatch`.
- The Python driver runs only the engine requested and fails when it is unavailable: `pyyaml-safe` requires PyYAML, `pyyaml-csafe` requires PyYAML built with libyaml, and the built-in YAML 1.1 parser is selected explicitly with `--engine builtin-yaml11`. The undocumented `yamlprims` engine is removed.
- The Go drivers report the module versions recorded in their build information, the Node driver reports both js-yaml and Node versions and fails its handshake when js-yaml is not installed, and the Rust driver pins `yamlprims` exactly.
- The `yamlprims-go` driver uses yamlprims v0.3.0.
- `make check` is the non-mutating CI gate; `make pr-final` adds a required public-surface scan of the tree, commits, pull-request text and branch name; `make prepush` and the pre-push hook run that scan when a catalog is configured.
- CI scans pull requests, including forks, with the scanner's built-in baseline.
- Containment scratch directories are named `docpresser-run-*`.

## [0.1.0] - 2026-11-17

- Add the Docpresser CLI foundation, embedded application identity, and runtime diagnostics.
- Add pack schemas, `structcorpus` pin hydration, deterministic synthetic workload generation, and pack validation.
- Add out-of-process containment supervision, line-delimited JSON driver execution, and schema-validated NDJSON evidence recording.
- Add the independent four-axis verifier (`correctness`, `containment`, `migration_divergence`, `telemetry`), summary reporting, and Wave 1 YAML/JSON subject drivers.

[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/3leaps/docpresser/releases/tag/v0.1.0
