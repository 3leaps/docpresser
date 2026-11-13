# Foundation provenance

## Source baseline

Docpresser derives its CLI foundation from:

[`fulmenhq/forge-microtool-gimlet`](https://github.com/fulmenhq/forge-microtool-gimlet)

The source pin is commit
`b0f95f9733805ceee5ea16d52b035021e3245531` (`v0.2.0`). The repository retains
verbatim dual-license texts in [LICENSE-MIT](../../LICENSE-MIT) and
[LICENSE-APACHE](../../LICENSE-APACHE) and records trademark and template
provenance notices in [NOTICE](../../NOTICE). The source pin identifies the
imported foundation; it does not identify a Docpresser release or establish a
qualification result.

## Application and dependency pins

The application module is `github.com/3leaps/docpresser`. Its embedded identity
is sourced from [`.fulmen/app.yaml`](../../.fulmen/app.yaml): vendor `3leaps`,
binary and configuration name `docpresser`, and environment prefix
`DOCPRESSER_`.

The pinned Go module and tool baseline is:

- Go `1.26.10` (`CGO_ENABLED=0`);
- `github.com/fulmenhq/gofulmen v0.3.6`, resolved by the release tag to commit
  `8ecab1b6aa075a04fb3213059e71486fc87ca721`;
- `goneat v0.6.8`.

The CI toolchain is the digest-pinned multi-architecture image:

```text
ghcr.io/fulmenhq/goneat-tools-runner-glibc:v0.5.15@sha256:8c6324556e0e8c951b0ad2225f1cb87fad13445b6dcbc83717ace41c6959608e
```

That image supplies the Go toolchain (`go1.26.10`) and formatting and linting
tools (`goneat v0.6.8`, `actionlint`, `yamlfmt`, `prettier`, `shfmt`,
`checkmake`, `python3`, `jq`) used by CI. CI sets `GOTOOLCHAIN=local` so Go uses
the bundled version.

## Corpus pin

The Wave 1 structured-document corpus pin is:

- [`3leaps/structcorpus`](https://github.com/3leaps/structcorpus) commit
  `b52fcd090d6879a0bd58ec5a5496c6a4de90504e`.

## Verification

`make verify-toolchain` checks the Go version declared by `go.mod` and the
`goneat` version declared by the Makefile. `make check` is non-mutating and
includes toolchain verification, application-identity checks, embedded-identity
verification, verbatim license and notice checks, public-surface content checks,
formatting, linting, unit and import-boundary tests, dependency-license checks,
multi-platform builds, and a standalone-binary check outside the checkout.
