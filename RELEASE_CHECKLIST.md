# Release Checklist

Checklist for Docpresser releases. Docpresser v0.x releases are source-only
signed Git tags: no binaries, archives, checksums, or package-manager
artifacts are published.

## Pre-Release

### Version Planning

- [ ] All planned changes merged to `main` and tested
- [ ] Breaking changes documented
- [ ] Version number decided (semantic versioning: MAJOR.MINOR.PATCH)

### Code Quality

- [ ] Non-mutating gate passing: `make check`
- [ ] Pre-push gate passing: `make prepush`
- [ ] Release tooling suites passing (throwaway keys only; needs gpg, git,
      python3, jq, make, and Decernor via `DOCPRESSER_DECERNOR_BIN`):
      `make release-tooling-test`
- [ ] Committed public pin agrees with both fingerprint anchors:
      `make release-anchors-check`
- [ ] Public-surface scan with the catalog passing for the release pull
      request: `make pr-final` (needs `LIMENSAFE_CATALOG_PATH` and
      `DOCPRESSER_SURFACE_PR_TEXT`)
- [ ] Sensitive local data reviewed against the
      [3 Leaps Sensitive Local Data Policy](https://github.com/3leaps/oss-policies/blob/main/SENSITIVE-LOCAL-DATA.md);
      none is stored inside the repository tree relying only on `.gitignore`.

### Documentation

- [ ] `README.md` reviewed and updated
- [ ] CLI help text accurate
- [ ] `CHANGELOG.md` has a dated `## [<version>] - YYYY-MM-DD` section with
      the release entries

### Dependencies

- [ ] `go.mod` dependencies reviewed; no local `replace` directives
- [ ] `go mod tidy` leaves no diff
- [ ] No known vulnerabilities in dependencies

## Release Preparation

### Version Updates

- [ ] Update `VERSION`: `make version-set VERSION=<version>`
- [ ] Update `version` in `.fulmen/app.yaml`, then regenerate the embedded
      mirror: `make sync-embedded-identity`
- [ ] Sync the changelog compare links: `make sync-changelog-links`
- [ ] Changelog matches `VERSION`: `make release-guard-release-surfaces`
- [ ] Version sanity check:
      `DOCPRESSER_RELEASE_TAG=v<version> make release-guard-signing-tag`

### Git Hygiene

- [ ] Release preparation merged to `main`; local checkout is on `main` and
      matches `origin/main`
- [ ] `git status` clean
- [ ] Every commit carries the attribution trailers

## Release Execution

### Signing Inputs

The tag is signed by `3 Leaps Infosec Team <infosec@3leaps.net>` with the
pinned signing subkey. Public trust material is committed:
`config/release/tagger-identity.txt`, `config/release/signing-subkey.txt`,
`docs/security/release-signing-keys.asc`, and
`keys/expected-fingerprints.{txt,ndjson}`. See
[docs/security/README.md](docs/security/README.md).

Set in the shell, never in the repository:

| Variable                             | Purpose                                                                                  |
| ------------------------------------ | ---------------------------------------------------------------------------------------- |
| `DOCPRESSER_RELEASE_TAG`             | Release tag `v<version>`; must match `VERSION`                                           |
| `DOCPRESSER_DECERNOR_BIN`            | Absolute path to `decernor` 0.1.8 or newer                                               |
| `DOCPRESSER_TAG_MESSAGE_DIR`         | Absolute directory outside the repository whose last component is the tag                |
| `DOCPRESSER_TAGGER_NAME`             | Tagger name; must match `config/release/tagger-identity.txt`                             |
| `DOCPRESSER_TAGGER_EMAIL`            | Tagger email; must match `config/release/tagger-identity.txt`                            |
| `DOCPRESSER_GPG_SIGNING_FINGERPRINT` | Approved primary fingerprint (40 uppercase hex)                                          |
| `DOCPRESSER_PGP_KEY_ID`              | Signing subkey selector `<subkey>!` matching `config/release/signing-subkey.txt`         |
| `DOCPRESSER_GPG_HOMEDIR`             | Dedicated `0700` GnuPG home outside the repository; never the default GnuPG home         |
| `LIMENSAFE_CATALOG_PATH`             | Public-surface catalog file outside the repository; required by `make release-preflight` |

Optional inputs (unset means the default shown):

| Variable                           | Default       | Purpose and constraints                                                                                                                                                                                 |
| ---------------------------------- | ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `DOCPRESSER_APPROVED_ENV_LOADER`   | unset         | Maintainer shell file sourced by the tag targets before their guards run. When set it must be an absolute, readable regular file (not a symlink) outside the repository, and it must exit successfully. |
| `DOCPRESSER_REQUIRE_TAG`           | `0`           | `1` makes `make release-guard-tag-version` require `DOCPRESSER_RELEASE_TAG` instead of defaulting to `v<VERSION>`.                                                                                      |
| `DOCPRESSER_LIMENSAFE_BIN`         | `limensafe`   | Absolute path to the public-surface scanner when it is not on `PATH`.                                                                                                                                   |
| `DOCPRESSER_SURFACE_SCOPE`         | `pr`          | `release` scans every commit message in history; the pull-request and release targets set it themselves.                                                                                                |
| `DOCPRESSER_SURFACE_BASE`          | `origin/main` | Base commit for the pull-request commit-message scan.                                                                                                                                                   |
| `DOCPRESSER_SURFACE_PR_TEXT`       | unset         | Non-empty file holding the pull-request title and body to scan.                                                                                                                                         |
| `DOCPRESSER_SURFACE_SCAN_REQUIRED` | `0`           | `1` fails instead of skipping when `LIMENSAFE_CATALOG_PATH` is unset; `make pr-final` and the release targets set it themselves.                                                                        |

Must stay unset: `DOCPRESSER_SURFACE_RELEASE_BASE` (the release scan refuses
any override of its base), `DOCPRESSER_TEST_FAIL_ANCHOR_INSTALL` (a
fault-injection hook used only by the tooling tests), and
`DOCPRESSER_SURFACE_BASELINE_ONLY` and `DOCPRESSER_SURFACE_BRANCH_NAME` (set
only by the CI baseline scan).

The tag comes only from `DOCPRESSER_RELEASE_TAG`. The version guard and the
tag targets refuse to start while the generic `RELEASE_TAG` is set, even to an
empty value, so `unset RELEASE_TAG` first.

### Tagging (maintainer machine)

- [ ] Remote tag-protection ruleset and immutable-releases setting:
      `make release-github-preflight` (needs a GitHub token with repository
      admin access, which can read the ruleset's bypass list)
- [ ] Pre-tag gate on the merged `main` commit (committed anchor verification,
      `make surface-scan-release`, changelog and compare links, GitHub
      preflight, content-check, dates check, clean-clone test outside the home
      directory, and `make check`): `make release-preflight`
- [ ] Prepare and review the external tag message:
      `make release-prepare-tag-message`
- [ ] Create and verify the local signed tag: `make release-tag`
- [ ] Verify the local tag against the committed pin: `make release-verify-tag`
- [ ] Publish and verify the tag (local signature against the committed pin,
      remote object, GitHub `valid`): `make release-push-tag`. The push reports
      the tag ruleset but does not enforce it; `make release-preflight` does,
      so rerun it if anything changed since.
- [ ] Re-verify the published tag from a clean checkout:
      `make release-verify-published-tag` (and
      `make release-verify-remote-tag` to compare local and remote objects)

### Public Pin Maintenance

Only when the release signing key changes. Each target refuses to overwrite
existing material; replacing a pin or anchor is a reviewed change that deletes
the old file first.

- [ ] Export the approved public key to an absent pin: `make release-export-pin`
- [ ] Validate the existing pin (read-only): `make release-validate-pin`
- [ ] Derive absent fingerprint anchors from the validated pin:
      `make release-insert-anchors`

## Post-Release

- [ ] `go install github.com/3leaps/docpresser/cmd/docpresser@v<version>`
      resolves the tagged source
- [ ] Monitor GitHub issues for release-related bugs

## Notes

- Never skip tests, hooks, or review to speed up a release.
- Signing and tag publication are maintainer actions.
