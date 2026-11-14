# Docpresser Subject Drivers (`docpresser-driver/v0`)

Each directory under `drivers/` is an **independent, standalone subject driver** (with its own `go.mod`, `Cargo.toml`, `package.json`, or runtime script). The `docpresser` coordinator binary (`CGO_ENABLED=0`) never links target or baseline parser libraries in-process; instead, `docpresser run` invokes subject drivers out-of-process under `internal/containment`.

## Driver Inventory (Wave 1)

| Directory                 | Subject ID                                   | Role              | Language / Engine                                          | Build / Command                                                          |
| :------------------------ | :------------------------------------------- | :---------------- | :--------------------------------------------------------- | :----------------------------------------------------------------------- |
| `drivers/yamlprims-go/`   | `yamlprims-go`                               | Target            | Go (`3leaps/yamlprims` + `3leaps/canonprims`)              | `cd drivers/yamlprims-go && go build -o ../../bin/driver-yamlprims-go .` |
| `drivers/yamlprims-rust/` | `yamlprims-rust`                             | Target            | Rust (`yamlprims` crate)                                   | `cd drivers/yamlprims-rust && cargo build --release`                     |
| `drivers/go-yamlv3/`      | `go-yamlv3`                                  | Baseline          | Go (`gopkg.in/yaml.v3`)                                    | `cd drivers/go-yamlv3 && go build -o ../../bin/driver-go-yamlv3 .`       |
| `drivers/python-yaml/`    | `python-pyyaml-safe` / `python-pyyaml-csafe` | Baseline / Target | Python 3 (`PyYAML SafeLoader`, `CSafeLoader`, `yamlprims`) | `python3 drivers/python-yaml/driver.py --engine pyyaml-safe`             |
| `drivers/node-yaml/`      | `node-yaml`                                  | Baseline          | Node.js (`js-yaml` / native JCS)                           | `node drivers/node-yaml/driver.mjs`                                      |

## Wire Protocol (`docpresser-driver/v0`)

1. **Handshake (`--docpresser-handshake`):**
   - Invoked once at the start of `docpresser run` to record subject identity, version, language, engine, formats, and supported operations in `run-manifest.json`.
2. **Per-case Execution (`stdin` -> `stdout`):**
   - `docpresser` writes a single JSON `CaseRequest` to `stdin` (with `input_base64`, `format`, `operation`, `profile`, and optional `budgets`).
   - The driver writes a single JSON `CaseResponse` to `stdout` (`status` in `ok | rejected | refused_bounded | unsupported | crash`, plus `canonical_value`, `output_sha256`, `output_bytes`, and structured `error` when rejected/refused).
