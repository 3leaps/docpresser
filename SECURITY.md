# Security

Docpresser is experimental (v0.x). Fixes land on `main` and ship in the next
release; earlier releases are not patched.

Report potentially sensitive security issues privately to
[security@3leaps.net](mailto:security@3leaps.net). Include the affected version
or commit, the command and pack involved, and a minimal synthetic reproduction
when available. Do not post credentials, private documents, or private
configuration in public issues.

## Driver containment

`docpresser run` executes subject drivers as separate processes. Each driver
runs with a wall-clock timeout, a temporary working directory, a scrubbed
environment, and capped stdout and stderr. On Unix, the driver's whole process
group is terminated on timeout and peak memory use is measured after it exits.
On Linux the reported peak is never lower than the memory docpresser itself
held when it started the driver, so drivers smaller than that are
over-reported. On other platforms only the driver process is terminated and
no memory use is recorded. There is no network, filesystem, or enforced memory isolation, so
this is supervision, not a sandbox: run only drivers you trust, and treat
hostile inputs as able to exhaust host resources.

## Release verification

Release tags are signed. See
[release signing and verification](docs/security/README.md).
