# Docpresser contributor instructions

## Read first

1. Retrieve and read the assigned role from the canonical
   [3leaps Crucible role catalog](https://github.com/3leaps/crucible/tree/24c8920dfafdd8a977c8873597562e4ed20e3204/config/agentic/roles)
   (`v0.2.10`). Implementation uses
   [devlead](https://github.com/3leaps/crucible/blob/24c8920dfafdd8a977c8873597562e4ed20e3204/config/agentic/roles/devlead.yaml);
   separately assigned independent review uses
   [devrev](https://github.com/3leaps/crucible/blob/24c8920dfafdd8a977c8873597562e4ed20e3204/config/agentic/roles/devrev.yaml).
   Read the role contents explicitly; links alone do not load instructions.
   Record the role version and revision used. Do not vendor role copies into this
   repository.
2. [README.md](README.md) and [architecture](docs/architecture.md).
3. [First-wave build packet](docs/development/wave-one.md) and the specification
   relevant to the requested slice.

At task start, identify the assigned role, version, and instruction sources
loaded. Implement the requested scope. The author does not become an independent
reviewer by switching role labels. Relied-upon reviews follow the canonical
[fierce-collaboration review standard](https://github.com/3leaps/crucible/blob/24c8920dfafdd8a977c8873597562e4ed20e3204/docs/standards/fierce-collaboration-review.md).

## Repository conventions

- Language: Go; module `github.com/3leaps/docpresser`; binary/config name
  `docpresser`; environment prefix `DOCPRESSER_`.
- **Package naming rule:** never export or create a Go package named `structdoc`
  (which collides with struct-documentation tooling in the Go and Rust package
  ecosystems). Keep public copy opened with _"structured documents"_ rather than
  bare abbreviations.
- Toolchain and dependency versions are governed by `go.mod`, `go.sum`, the
  Makefile, and the digest-pinned CI image in [architecture](docs/architecture.md).
  Change pins explicitly and verify the resulting toolchain; do not silently
  float dependencies or subject revisions.
- Edit `.fulmen/app.yaml` and regenerate its embedded mirror with
  `make sync-embedded-identity`. Generated mirrors are not source files to edit.
- Keep public documentation evergreen: describe behavior, supported
  configurations, design constraints, and usage. Keep task plans, team
  assignments, priorities, and operational coordination outside the repository.

## Architecture and evidence

- Keep generator/pack loader, runner, containment supervisor, subject drivers,
  oracle, and recorder separate. The oracle must not import document parsers,
  subject drivers, generator logic, or the runner's execution model. Shared
  structural types are permitted and verified via Go dependency tests.
- Derive expected results from pinned corpus manifests (`3leaps/structcorpus`),
  format specifications, and hand-authored controls — independently of subject
  output. Never copy an incumbent engine's output into a golden file as truth;
  incumbent disagreements are classified as `migration_divergence` or subject
  findings, never as format truth.
- **Subject integrity and isolation.** Every subject driver runs out-of-process
  under bounded CPU, wall-clock, memory/RSS, and filesystem/network containment
  with the recorder owned by the parent coordinator. Do not link C parser
  libraries (`libyaml`, `libxml2`, `expat`) into the `docpresser` coordinator
  binary. An adapter named after a subject drives that subject's real entry
  points at a recorded version/digest; if a subject binary or runtime is
  unavailable, report `unsupported` or `inconclusive`, never a stub.
- Preserve detecting controls for missing/spurious items, mutated pack digests,
  silent duplicate-key overwrites, unrecorded subject crashes (`SIGSEGV`,
  `SIGABRT`, `SIGKILL`), memory-ceiling breaches, pin drift, and incomplete or
  empty runs. Confirm rejection at the intended assertion.
- Use 100% synthetic or pinned public-only corpus inputs. Never admit private,
  redacted, or credentialed material.

## Validation and output

- Run relevant tests and detecting controls, then the non-mutating `make check`
  gate before review. Use `fmt-check` for validation; `make fmt` mutates files.
- Offline checks must work without network access or external services.
  Requested subject campaigns must fail clearly when a required subject driver or
  containment runtime is unavailable; do not silently skip to green.
- Report commands, exit status, and unexercised profiles. Cross-compilation is
  build evidence; runtime and performance claims require native execution.
- Keep machine output on stdout and diagnostics on stderr.

## Public contributions

Use concise, factual commit and PR descriptions with relevant validation. Keep
private identifiers, local paths, team/bot names, non-public repository
references, and coordination narratives off public surfaces. Follow the
authorized branch/PR workflow; merge, tag, and release actions require
maintainer direction.

Every commit and PR body ends with these three lines, using the actual model and
bare role slug:

```text
Role: <assigned bare role>
Co-authored-by: <actual model> <noreply@3leaps.net>
Committer-of-Record: @3leapsdave
```
