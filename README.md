# Docpresser

Qualification and synthetic-workload harness for structured documents (YAML,
JSON/I-JSON/JCS, XML, XHTML, SVG; OOXML and PDF in later profiles).

**Status: initial architecture and build packet.** Commands, subject drivers, and
measurements described in the documentation below are planned until implemented
and verified by execution evidence.

Docpresser generates or binds deterministic workload packs (including pinned
[`3leaps/structcorpus`](https://github.com/3leaps/structcorpus) releases), drives
document readers, emitters, canonicalizers, and extractors inside bounded
out-of-process execution environments, and independently verifies retained
evidence against the selected input pack.

Results separate four distinct axes and never collapse them into a single
composite score:

- **Correctness:** outcome against the pack's format or profile expectations
  (`pass`, `fail`, `unsupported`, `inconclusive`).
- **Adversarial containment:** bounded structured refusal versus process crash,
  stack exhaustion, timeout, or out-of-memory termination under hostile inputs.
- **Migration divergence:** structured behavioral differences between dialects or
  engines (for example YAML 1.2.2 Core Schema versus legacy YAML 1.1 scalars and
  merge keys), reported without treating an incumbent engine as the format oracle.
- **Telemetry:** throughput, latency percentiles, peak physical RSS, and memory
  amplification ratios tied to recorded binary, toolchain, and host facts.

## Documentation

- [Architecture](docs/architecture.md)
- [First-wave build packet](docs/development/wave-one.md)

## Contributing

Read [AGENTS.md](AGENTS.md) alongside the canonical
[3leaps Crucible role catalog](https://github.com/3leaps/crucible/tree/24c8920dfafdd8a977c8873597562e4ed20e3204/config/agentic/roles)
and
[fierce-collaboration review standard](https://github.com/3leaps/crucible/blob/24c8920dfafdd8a977c8873597562e4ed20e3204/docs/standards/fierce-collaboration-review.md).

## License

Licensed under either of `Apache-2.0` or `MIT` at your option.
