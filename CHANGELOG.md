# Changelog

## [Unreleased]

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
