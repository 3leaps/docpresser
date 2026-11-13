# First-wave build packet

Status: planned work. This packet defines the slice order and acceptance gates
for Wave 1 of Docpresser (`YAML`, `JSON`/`I-JSON`/`JCS`, and the format-neutral
harness core).

Read [Architecture](../architecture.md) for foundation pins, package ownership,
containment boundaries, and oracle rules.

## Slice 1 — Bootstrap the Go CLI and repository hygiene

1. Import `fulmenhq/forge-microtool-gimlet` (`v0.2.0`,
   `b0f95f9733805ceee5ea16d52b035021e3245531`) from a disposable copy while
   preserving this repository's `.git`, remote, and documentation.
2. Set `.fulmen/app.yaml` to vendor `3leaps`, binary/config `docpresser`, env
   prefix `DOCPRESSER_`, module `github.com/3leaps/docpresser`, and regenerate
   embedded identity (`make sync-embedded-identity`). Ensure `CGO_ENABLED=0` for
   the coordinator binary and confirm no package is named `structdoc`.
3. Configure verbatim `LICENSE-MIT`, verbatim `LICENSE-APACHE`, and `NOTICE`
   (retaining Gimlet MIT provenance), plus `docs/development/foundation.md`
   recording exact Go, `gofulmen`, `goneat`, and CI runner image digests.
4. Wire non-mutating `make check` (`verify-toolchain`, `fmt-check`, lint, schema
   validation, unit tests, Go import-boundary check, standalone binary smoke
   check outside the checkout, and public-surface hygiene checks).

**Acceptance:** `make check` passes offline; `./bin/docpresser version --extended`,
`envinfo`, and `doctor` succeed outside the checkout with machine output on
stdout and diagnostics on stderr.

## Slice 2 — Pack schemas, `structcorpus` pin binder, seeded generator, recorder, and independent oracle

1. Author JSON Schemas under `schemas/docpresser/v0/` for `pack.json`, NDJSON
   run-observation records, `run-manifest.json`, and `verification-report.json`.
   Reject unknown fields and unknown schema versions.
2. Implement `docpresser bind-corpus` (hydrating and verifying a pinned
   `3leaps/structcorpus` checkout into a content-hashed `pack.json`),
   `docpresser generate` (seeded deterministic synthetic YAML/JSON workload
   packs: `k8s-bundle`, `openapi-graph`, `diabolical-alias-pyramid`,
   `yaml11-migration`), and `docpresser validate --pack <dir>`.
3. Implement `internal/evidence` immutable NDJSON recorder and `internal/oracle`
   offline `docpresser verify --run <dir> --pack <dir>` +
   `docpresser summarize --run <dir> --pack <dir> [--format json|markdown]`.
4. Author small hand-crafted specimens in `corpus/v0/` covering valid runs and
   detecting controls: mutated `pack_sha256`, missing case observation, spurious
   case, silent wrong digest reported as success, unrecorded crash/incomplete
   manifest, containment RSS breach, and dialect `migration_divergence`.

**Acceptance:** identical seed + parameters yield byte-identical `pack.json`;
every `corpus/v0/` negative specimen is rejected for its exact named reason; Go
dependency tests prove `internal/oracle` imports zero driver, containment,
generator, or runner packages.

## Slice 3 — Out-of-process containment supervisor and NDJSON driver protocol

1. Implement `internal/containment` and `internal/driver` for single-case
   contained execution and batch throughput trials (wall-clock timeout, child
   RSS/memory ceiling, isolated empty/read-only working dir, child `rusage` /
   wait-status / signal capture).
2. Build a synthetic Go test-fixture driver (`internal/driver/testdata/faultdriver`)
   never reported as a real subject, capable of deterministic behaviors on
   command: clean pass, structured budget refusal, `SIGABRT` / panic crash,
   deliberate memory allocation spike, infinite hang (timeout), and truncated
   stdout JSON.

**Acceptance:** parent coordinator and evidence recorder survive every fault mode
of `faultdriver` without corruption; `verify` classifies clean refusal as
`refused_bounded` and classifies crash, OOM/RSS breach, hang, and malformed
protocol output under their exact failure/containment categories.

## Slice 4 — Wave 1 YAML & I-JSON/JCS subject drivers and `libyaml` comparative campaign

1. Implement thin, pinned external subject drivers under `drivers/` (built in
   isolated workspaces so `docpresser` `go.mod` stays pure):
   - **3leaps subjects:** `yamlprims` (`v0.2.1` — Rust crate, pure-Go `yaml`
     twin, Python `PyO3`, and TypeScript WASM) and `canondocs` / `canonprims`
     (`YAML -> I-JSON -> JCS`).
   - **Incumbent comparison baselines:** `PyYAML` (`CSafeLoader` backed by C
     `libyaml` + pure-Python `SafeLoader`), Rust `serde-yaml` (`unsafe-libyaml`),
     Go `gopkg.in/yaml.v3`, and Node `js-yaml` / `yaml`.
2. Execute and verify the four Wave 1 profiles:
   - Pinned `3leaps/structcorpus` conformance profile (`61,366` cases).
   - Diabolical amplification & deep-nesting containment profile (recording
     sub-millisecond bounded refusal in `yamlprims` versus RSS blowup, stack
     exhaustion, or timeout in unbounded incumbents).
   - YAML 1.1 $\rightarrow$ YAML 1.2.2 Core `migration_divergence` & emitter
     `Yaml11Safe` round-trip profile.
   - Multi-megabyte synthetic workload throughput & RSS profile (`10 KiB`–`50 MiB`).

**Acceptance:** all four profiles execute reproducibly from a clean checkout,
`docpresser verify` passes on retained run artifacts, and `docpresser summarize`
emits both machine-readable JSON and evergreen Markdown tables suitable for
downstream documentation in `3leaps/yamlprims` and `3leaps/canondocs`.
