# Compatibility policy

This policy defines the stable contract beginning with `v1.0.0`.

## Semantic versioning

Sevlumen ORM follows semantic versioning for the public Go module and shipped command-line tools.

- Patch releases fix defects and security issues without intentionally breaking supported behavior.
- Minor releases add backward-compatible APIs and features and may deprecate existing APIs.
- Major releases may remove deprecated APIs or change incompatible contracts after migration guidance is published.

Prereleases may change before `v1.0.0`. Every intentional public-surface change must update the reviewed API baseline and explain the compatibility impact.

## Public Go API

Exported declarations in supported packages are recorded under `api/v1/` and verified in CI. The stable packages are:

- `github.com/sevlumen/orm`;
- `entity`;
- `generator`;
- `migration`;
- `migration/artifact`;
- `postgres`;
- `postgres/query`;
- `postgres/runner`;
- `schema`.

Packages under `internal/`, commands under `cmd/`, tests, examples, and implementation details are not public Go API.

Within v1, maintainers do not intentionally:

- remove or rename an exported declaration;
- change an exported function or method signature incompatibly;
- add a method to a public interface when that would break third-party implementations;
- remove or change the type of an exported struct field;
- change a documented constant value incompatibly.

Adding exported struct fields can break unkeyed composite literals. Consumers should use keyed literals for public configuration and schema structs. Maintainers will review field additions as compatibility-sensitive and document them.

## Behavioral compatibility

Compilation alone does not define compatibility. The following are reviewed as public behavior:

- SQL parameterization and identifier quoting;
- cardinality and not-found behavior;
- transaction, cancellation, and observer semantics;
- deterministic SQL and code generation;
- migration risk classification and ordering;
- artifact checksum and history-prefix validation;
- relation query-count guarantees;
- error classification through `errors.Is` and `errors.As`.

Exact error-message wording, benchmark timing, and undocumented SQL whitespace are not stable unless a command JSON schema or test explicitly defines them.

## CLI compatibility

Human-readable output is intended for people and may gain detail. Automation should use `--json`.

The JSON envelope version is stable:

```json
{"version":1,"command":"status","result":{}}
```

Within JSON version 1:

- existing fields retain their meaning and type;
- new optional fields may be added;
- field ordering is deterministic but consumers should parse JSON rather than compare text;
- unknown fields should be tolerated by consumers;
- exit codes `0`, `1`, and `2` retain their documented categories.

A breaking JSON change requires a new envelope version or a new major release.

## Configuration compatibility

`orm` config version 1 and rename-intent version 1 are strict inputs. Unknown fields are rejected intentionally to catch mistakes.

Backward-compatible releases may add optional fields only when older tools can safely reject the newer config. A changed field type, removed field, or changed default that alters execution safety requires a new config version or major release.

Flags override configuration. Existing flag meaning is stable within v1. New flags may be added.

## Snapshot and artifact compatibility

Snapshot version 1 and migration artifact format version 1 are persistent contracts.

Within v1:

- previously valid artifacts remain readable and verifiable;
- checksums continue to cover the complete artifact payload;
- applied history remains an exact prefix of local artifacts;
- generated artifacts remain deterministic for equivalent canonical schemas;
- existing migrations are never silently rewritten.

A format that old tools could misinterpret must use a new version. Readers fail closed on unsupported versions, unknown fields, trailing data, non-regular files, size limits, and checksum mismatch.

Generated SQL itself is committed deployment input. Do not regenerate or edit an applied migration. Create a new migration instead.

## Database compatibility

The supported PostgreSQL range is 14 through 18. Generated SQL must remain valid across that range unless a feature is explicitly documented as requiring a newer major and is guarded accordingly.

PostgreSQL patch releases are expected to remain compatible. Security and correctness fixes may require updating the digest-pinned CI image without changing the supported major.

## Go compatibility

The minimum Go version for v1 is 1.25. CI also tests the current stable Go release. The minimum may increase in a future minor release only with changelog, support-policy, and upgrade-guide updates and a practical maintenance reason.

## Deprecation

A deprecated API is documented in Go comments and the changelog. It remains available for at least one subsequent minor release. Removal occurs only in a future major version unless keeping it would preserve a confirmed security vulnerability; security exceptions require an advisory and migration instructions.

## Trusted SQL boundary

Raw SQL, `TrustedSQL`, migration SQL, type overrides, defaults, checks, generated expressions, and expression indexes are developer-authored inputs. Their text is not a stable sanitization service and must not receive untrusted runtime data. Typed values and generated/validated identifiers are the injection-protected path.
