# Sevlumen ORM release

This release contains reproducible command archives, a tracked-source archive, an SPDX 2.3 SBOM, a release manifest, and SHA-256 checksums.

## Compatibility

- Go 1.25 or newer.
- PostgreSQL 14 through 18.
- `orm` and `ormgen` binaries for Linux, macOS, and Windows on amd64 and arm64.

Review [the changelog](../CHANGELOG.md), [compatibility policy](compatibility.md), [upgrade guide](upgrade.md), and [recovery runbook](recovery.md) before deployment.

## Security boundary

Typed query and mutation values are PostgreSQL positional parameters. Generated and validated identifiers are quoted or rejected fail-closed. Raw SQL, `TrustedSQL`, migration SQL, type overrides, defaults, checks, generated expressions, and expression indexes remain trusted developer-authored inputs and must never receive untrusted runtime data.

## Assets

Each binary archive contains only:

- `orm` or `orm.exe`;
- `ormgen` or `ormgen.exe`;
- `README.md`;
- `LICENSE`.

The release also publishes:

- `sevlumen-orm_<version>_source.tar.gz`: files tracked by the tagged commit;
- `sevlumen-orm_<version>_sbom.spdx.json`: deterministic SPDX 2.3 module SBOM;
- `release-manifest.json`: version, commit, timestamp, target list, size, and digest metadata;
- `SHA256SUMS`: sorted SHA-256 digests for every published payload.

Release archives are built twice from the same tagged commit with `CGO_ENABLED=0`, `-trimpath`, VCS embedding disabled, an empty Go build ID, normalized archive metadata, and the commit timestamp. Publication stops when the two output directories differ by one byte.

## Verify checksums

Download all release assets into one directory and run:

```text
sha256sum -c SHA256SUMS
```

On macOS, use a SHA-256 tool that accepts the same two-space checksum format or verify each digest independently.

The repository's verifier additionally checks the exact file set, release manifest, archive path traversal, symlinks, non-regular entries, and required binary payloads:

```text
go run github.com/sevlumen/orm/cmd/releaseverify@<tag> -dir ./downloaded-assets
```

## Verify GitHub attestations

Every payload receives GitHub-hosted Sigstore build-provenance attestation. Every checksummed payload also receives an SBOM attestation tied to the published SPDX document.

With GitHub CLI:

```text
gh attestation verify ./sevlumen-orm_<version>_linux_amd64.tar.gz \
  --repo sevlumen/orm
```

Verify any downloaded asset the same way. The verifier checks the signed attestation against GitHub's trust root and the expected repository identity.

GitHub release verification can also validate an asset against the release metadata when that feature is available for the repository:

```text
gh release verify-asset <tag> ./sevlumen-orm_<version>_linux_amd64.tar.gz \
  --repo sevlumen/orm
```

Do not rely only on a file name or a checksum copied from an untrusted page. Obtain the release, checksums, and attestations through the repository's GitHub release and verify repository identity.

## Verify binary metadata

After extracting an archive:

```text
./orm version --json
./ormgen -version-json
```

Confirm:

- `version` exactly equals the release tag;
- `commit` equals the tagged commit SHA;
- `dirty` is `false`;
- `goos` and `goarch` match the downloaded asset.

## Release process

The publishing workflow runs only for a pushed annotated semantic-version tag. It reruns:

- module and formatting checks;
- vet and race tests;
- vulnerability analysis;
- fuzz smoke;
- public API and documentation checks;
- PostgreSQL 14 and 18 integration/security tests;
- two independent release builds and artifact verification.

Only after those jobs pass does the publish job receive permission to create attestations and the GitHub release.

## Release candidates

Tags containing a prerelease suffix, such as `v1.0.0-rc.1`, are published as prereleases. Release candidates are not the stable support baseline. The final `v1.0.0` tag requires the maintained real-application validation report from the same release commit.

## Bad release, yank, or replacement

Published assets are immutable evidence. Never replace an asset under an existing tag or reuse a version.

For a bad release:

1. stop rollout and mark the GitHub release as unsupported or withdrawn;
2. preserve the tag, assets, checksums, attestations, and affected-environment evidence;
3. follow the migration recovery runbook when a database change was applied;
4. fix the defect with regression tests;
5. publish a new patch or release-candidate tag with new assets and attestations;
6. publish a security advisory when confidentiality or integrity was affected.

Deleting a release page does not make already downloaded artifacts disappear. Communicate the replacement version explicitly.

## Known limits

- Reproducibility means byte-for-byte equality for the same source, exact Go toolchain, target matrix, and release inputs. A different Go compiler version may produce different binaries.
- GitHub attestations establish workflow and repository provenance; they do not prove that trusted developer-authored SQL is harmless.
- Generated rollback SQL cannot reconstruct data removed by destructive migrations.
