# Architecture

Status: target design. Version 0.2.1 implements part of it: drivers run as
separate processes with a wall-clock timeout, a temporary working directory, a
scrubbed environment, and capped output; on Unix the process group is
terminated on timeout and peak memory is measured after exit. Network
isolation, enforced memory ceilings, read-only working directories, and
container execution described below are target design, not yet implemented.
No benchmark result is claimed until verified execution evidence is recorded.

## Foundation and pins

Docpresser is a Go CLI (`github.com/3leaps/docpresser`) for generating or binding
deterministic structured-document workload packs, executing subject engines
inside bounded out-of-process containment, recording execution
evidence, and independently verifying that evidence against the selected input
pack.

Application identity (`.fulmen/app.yaml`): vendor `3leaps`, binary and
configuration name `docpresser`, environment prefix `DOCPRESSER_`, repository
category `cli`. `version --extended`, `envinfo`, and `doctor` expose runtime and
toolchain diagnostics on stderr.

Bootstrap baseline:

- CLI template: [`fulmenhq/forge-microtool-gimlet`](https://github.com/fulmenhq/forge-microtool-gimlet)
  at `v0.2.0` (`b0f95f9733805ceee5ea16d52b035021e3245531`), retaining MIT
  provenance notices while dual-licensing Docpresser contributions under
  `MIT OR Apache-2.0`.
- Go toolchain baseline: Go `1.26.10`+ (`GOTOOLCHAIN=local` in CI).
- Foundation library: `github.com/fulmenhq/gofulmen v0.3.6`
  (`8ecab1b6aa075a04fb3213059e71486fc87ca721`) for embedded identity, typed
  configuration, structured stderr logging, signal handling, catalog exit codes,
  offline JSON Schema validation, and bounded path discovery.
- Maintainer & CI runner image:
  `ghcr.io/fulmenhq/goneat-tools-runner-glibc:v0.5.15@sha256:8c6324556e0e8c951b0ad2225f1cb87fad13445b6dcbc83717ace41c6959608e`
  (`goneat v0.6.8`).
- Initial corpus pin (Wave 1): [`3leaps/structcorpus`](https://github.com/3leaps/structcorpus)
  commit `b52fcd090d6879a0bd58ec5a5496c6a4de90504e` (upgrading to a signed
  `structcorpus` release tag once published).

If the maintainer approves a newer foundation or runner pin during bootstrap,
record the exact version, commit/digest, and verification output in
`docs/development/foundation.md`.

## Package boundaries

```text
cmd/docpresser/                   CLI entry point
internal/cmd/                     commands and exit-code mapping
internal/config/                  typed runtime and campaign configuration
internal/pack/                    pack.json manifest, corpus-pin binding, validation
internal/generate/                deterministic seeded synthetic workload generators
internal/containment/             subprocess supervision (timeout, output caps), RSS & signal capture
internal/driver/                  line-delimited JSON / batch-manifest driver client
internal/evidence/                NDJSON evidence recorders and run inventories
internal/oracle/                  independent correctness, containment, and divergence verifier
internal/runner/                  profile admission, warm/cold trial scheduling, orchestration
internal/assets/                  embedded application identity
drivers/                          optional standalone subject-driver harnesses (built separately)
corpus/v0/                        tiny hand-authored positive and negative evidence specimens
schemas/docpresser/v0/            pack, driver-protocol, and evidence JSON Schemas
profiles/                         named budget, containment, and workload profiles
pins/                             pinned upstream corpus commits/tags and SHA-256 inventories
```

Go import-graph tests enforce two strict boundaries:

1. `internal/oracle` never imports `internal/driver`, `internal/containment`,
   `internal/runner`, `internal/generate`, `internal/evidence` writers, or any
   document parser library.
2. No package in `github.com/3leaps/docpresser` exports a package named
   `structdoc` or links C parser libraries (`cgo` disabled by default for the
   coordinator binary: `CGO_ENABLED=0`).

## Input packs: corpus hydration and synthetic workloads

A pack directory contains `pack.json` plus content-addressed or indexed inputs
and expected-property declarations. `pack.json` is written only after all inputs
are finalized and records raw-byte SHA-256 digests, byte lengths, document
counts, format family (`yaml`, `json`, `xml`, `xhtml`, `svg`, `ooxml`, `pdf`),
and tier (`conformance`, `stress`, `migration`, `workload`).

Docpresser supports two pack sources:

1. **Pinned `structcorpus` hydration (`bind-corpus`):** invokes a pinned
   `3leaps/structcorpus` checkout (`python3 tools/corpus.py hydrate` + `verify`)
   into an isolated pack directory and binds the exact commit, profile revision,
   and hydrated file digests into `pack.json`.
2. **Seeded synthetic scale packs (`generate`):** deterministically synthesizes
   large structured documents (`10 KiB` to `100 MiB` multi-document YAML
   streams, OpenAPI/schema trees, JSON/I-JSON payloads, deep XML/XHTML/SVG trees,
   and later OOXML/PDF containers) from a recorded `uint64` seed and generator
   version, together with independent invariant/digest expectations.

`verify` and `summarize` require both `--run <run-dir>` and `--pack <pack-dir>`
and verify that the run's recorded `pack_sha256` matches the supplied pack. A run
directory cannot supply its own input authority.

## Out-of-process driver protocol and crash/OOM containment

Subject engines execute as separate OS processes invoked by
`internal/containment` and `internal/driver`. Rootless containers for
untrusted/C incumbent stress profiles are target design, not yet implemented.

- **Single-case containment mode (conformance, hostile/diabolical bombs,
  migration audits):** runs with a strict wall-clock timeout, an empty
  temporary working directory, a scrubbed environment, and capped output. An
  enforced OS memory/RSS ceiling, a read-only scratch directory, and network
  isolation are target design, not yet implemented. The parent coordinator owns the NDJSON evidence log outside the worker process. If
  a subject triggers `SIGSEGV`, `SIGABRT`, C-stack overflow, OOM-kill
  (`SIGKILL`), or timeout on a billion-laughs alias/entity bomb, the parent
  survives intact and records the exact wait status, signal, elapsed duration,
  and peak child RSS (`getrusage` on Unix; cgroup, Job Object, and `sysprims`
  evidence are target design).
- **Batch throughput mode (warm/cold latency & RSS telemetry on valid
  workloads):** streams a bounded trial manifest to the driver process over
  NDJSON stdin/stdout, recording per-iteration parse/emit/canonicalize nanos,
  output digest (so speed is never measured on a silently skipped or truncated
  parse), and child peak RSS.

Logical driver operations (each subject declares which operations it supports;
unimplemented operations record `unsupported`):

| Operation        | Input                                        | Output / Evidence                                                                    |
| ---------------- | -------------------------------------------- | ------------------------------------------------------------------------------------ |
| `load_value`     | raw bytes + profile limits/options           | canonical value digest or normalized value wire + structured error category/location |
| `load_events`    | raw bytes + profile limits                   | event count + canonical `structcorpus-events/1` digest or structured error           |
| `emit_canonical` | raw bytes or canonical value + emit options  | emitted bytes SHA-256 + byte length + re-parse round-trip check                      |
| `project_ijson`  | raw bytes + limits/options                   | RFC 7493 I-JSON UTF-8 bytes SHA-256 or fail-closed refusal code                      |
| `project_jcs`    | raw bytes + format (`json`/`yaml`)           | RFC 8785 JCS UTF-8 bytes SHA-256 or fail-closed refusal code                         |
| `lint_migration` | raw bytes + dialect rule set (e.g. `yaml11`) | structured hazard findings with `line:column` spans                                  |
| `extract_text`   | document bytes + container limits (Wave 2/3) | extracted UTF-8 digest, structural metadata counts, or bounded refusal               |

## Four-axis verification and reporting (`verify` / `summarize`)

The independent oracle evaluates retained evidence across four distinct axes:

1. **`correctness`:** `pass`, `fail`, `unsupported`, or `inconclusive` against
   the pack's normative expectations (including detection of silent duplicate-key
   overwrites, float/integer corruption, or truncated multi-document streams).
2. **`containment`:** for hazardous/amplification cases, distinguishes
   `refused_bounded` (clean error within configured time and memory budgets) from
   `oom_killed`, `crash_signal`, `timeout_exceeded`, and
   `amplification_exceeded` (peak RSS or output exceeding declared amplification
   ceiling).
3. **`migration_divergence`:** compares a target engine profile against a named
   incumbent baseline on dialect-sensitive inputs (for example YAML 1.2.2 Core
   versus `libyaml` YAML 1.1 scalars and `<<` merge keys), emitting categorized
   migration findings rather than pass/fail verdicts.
4. **`telemetry`:** reports input sizes, throughput (`MiB/s`, `docs/s`),
   `p50`/`p95`/`p99` latency, `peak_rss_bytes`, and `peak_rss_bytes / input_bytes`
   amplification alongside recorded host CPU/OS, binary SHA-256, library version,
   and toolchain metadata.
