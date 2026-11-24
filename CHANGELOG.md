# Changelog

## [Unreleased]

- `make release-prepare-tag-message` drafts a new tag message from the release's dated `CHANGELOG.md` section, copied as written after the changelog passes its release guard. The draft carries a review-marker line that tagging refuses until it is deleted.

## [0.2.4] - 2026-11-22

- New `serde-yaml-rust` baseline driver loads YAML through `serde_yaml` 0.9.34+deprecated's public untyped API: `from_slice` into `serde_yaml::Value` with no profile and for `yamlprims-core-merge-as-key`, the `Deserializer::from_slice` document iterator for `yaml-core`, and `apply_merge` for `yamlprims-core-merge-apply`. It reports the same value notation as the `yamlprims-rust` driver. Its handshake reports the `serde_yaml` and `unsafe-libyaml` versions resolved by its committed `Cargo.lock`, and its build fails unless the lock matches the exact `serde_yaml` pin. `serde_yaml` keeps merge keys as ordinary keys and exposes no resource limits, so every other profile, including unknown names and profiles that reject merge keys or set limits, answers `unsupported` with `unsupported_profile`. Errors are classified from `serde_yaml`'s fixed diagnostics without matching input text quoted in a message.
- `bind-corpus` reads structcorpus case budgets under their actual field names (`wall_ms`, `memory_bytes`, `output_bytes`). Previously it read other names, so a declared budget bound as zero and displaced the containment defaults. A budget object that is null, empty, or has an unknown field or a value that is not a positive integer now fails the bind, and on the containment axis an undeclared budget takes the containment default. Binding the structcorpus `stress` tier still fails: its resource-ladder cases carry no value expectations.
- `bind-corpus` binds the corpus from a snapshot of the checked-out commit's tree, read with git plumbing with replacement objects disabled, and checks every blob against its object id; working-tree, ignored, index-hidden and replaced content cannot enter the pack. The corpus root must be the top of its own clean git checkout, git environment variables that redirect the repository are ignored, and symbolic links or submodules in the corpus tree are refused. Previously, when the commit could not be read it recorded a fixed default commit, and it read files from the working tree.
- `make driver-serde-yaml-rust` builds and tests the driver with `--locked`; CI builds and tests both Rust drivers on Linux and macOS with each driver's minimum Rust version, and the cross-driver unsupported-operation test covers the new driver.

## [0.2.3] - 2026-11-21

- The `go-yamlv3`, `yamlprims-go`, `python-yaml` and `node-yaml` drivers no longer report an `error_phase` for an unsupported operation. That phase failed observation schema validation and stopped the run. 0.2.2 fixed only the `yamlprims-rust` driver, and its note that the Go drivers already omitted the phase was incorrect.
- The `node-yaml` driver answers `unsupported` for operations other than `load_value` and `project_jcs` instead of loading the input.
- A test sends every subject driver an unsupported operation through the same client path as `docpresser run` and validates the resulting observation against the observation schema. The Linux CI job installs `js-yaml` 4.1.0 for the `node-yaml` driver and requires every driver toolchain for this test.

## [0.2.2] - 2026-11-20

- The `yamlprims-rust` driver no longer reports an `error_phase` for an unsupported operation, matching the Go drivers. Previously its `driver` phase failed observation schema validation and stopped the run. `make driver-yamlprims-rust` and CI now also run the driver's tests, which check its error phases against the observation schema.
- On Linux, a driver's recorded peak memory is never lower than the memory docpresser held when it started the driver; `SECURITY.md` and the architecture document now say so. The fault-driver test sets its memory ceiling from the test process's own memory, tests out-of-memory detection by measured memory and by stderr separately, and allows a loaded host more time to start drivers in the handshake test.

## [0.2.1] - 2026-11-20

- The `yamlprims-rust` driver's `Cargo.lock` is committed, and the driver builds with `--locked` (`make driver-yamlprims-rust`). CI builds it on Linux and macOS with the minimum Rust version declared in its `Cargo.toml`.

## [0.2.0] - 2026-11-18

- Breaking (user configuration): the optional user configuration file is now JSON, `~/.config/3leaps/docpresser.json`, holding one object whose only key is `run_root` (a string). An existing `docpresser.yaml` is reported as an error instead of being read, and `DOCPRESSER_CONFIG_PATH` files must be JSON. docpresser no longer uses a YAML parser of its own.
- Breaking (run-manifest.json): `docpresser run` now calls each driver with `--docpresser-handshake` before any case and records what it reports. Each `subjects[]` entry carries `driver_subject` (the subject name the driver reports), `subject_version`, `language`, `engine`, `formats`, and `operations`; the `version` field is removed. The manifest also records `docpresser_version`, the docpresser build that wrote it. A failed or invalid handshake stops the run before any evidence is written. In manifests written by 0.1.0, `subjects[].version` is a fixed placeholder and does not identify the subject's version.
- A case response whose `subject_version` differs from the handshake is recorded as `protocol_error` with `subject_version_mismatch`.
- The Python driver runs only the engine requested and fails when it is unavailable: `pyyaml-safe` requires PyYAML, `pyyaml-csafe` requires PyYAML built with libyaml, and the built-in YAML 1.1 parser is selected explicitly with `--engine builtin-yaml11`. The undocumented `yamlprims` engine is removed.
- The Go drivers report the module versions recorded in their build information, the Node driver reports both js-yaml and Node versions and fails its handshake when js-yaml is not installed, and the Rust driver pins `yamlprims` exactly.
- The `yamlprims-go` and `yamlprims-rust` drivers use yamlprims v0.3.0; the Rust driver reports its exact yamlprims pin, and its build fails unless Cargo.lock resolves that same version.
- `make check` is the non-mutating CI gate; `make pr-final` adds a required public-surface scan of the tree, commits, pull-request text and branch name; `make prepush` and the pre-push hook run that scan when a catalog is configured.
- CI scans pull requests, including forks, with the scanner's built-in baseline.
- Containment scratch directories are named `docpresser-run-*`.
- Add `SECURITY.md`, and describe driver containment as implemented: wall-clock timeout, temporary working directory, scrubbed environment and capped output, with process-group termination and peak-memory measurement on Unix. Enforced memory ceilings, filesystem and network isolation, and container execution remain target design.

## [0.1.0] - 2026-11-17

- Add the Docpresser CLI foundation, embedded application identity, and runtime diagnostics.
- Add pack schemas, `structcorpus` pin hydration, deterministic synthetic workload generation, and pack validation.
- Add out-of-process containment supervision, line-delimited JSON driver execution, and schema-validated NDJSON evidence recording.
- Add the independent four-axis verifier (`correctness`, `containment`, `migration_divergence`, `telemetry`), summary reporting, and Wave 1 YAML/JSON subject drivers.

[Unreleased]: https://github.com/3leaps/docpresser/compare/v0.2.4...HEAD
[0.2.4]: https://github.com/3leaps/docpresser/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/3leaps/docpresser/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/3leaps/docpresser/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/3leaps/docpresser/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/3leaps/docpresser/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/3leaps/docpresser/releases/tag/v0.1.0
