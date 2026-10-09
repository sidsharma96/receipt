# Receipt JSON input contract

Implemented 2026-10-08, Phase 4. This records the current acceptance rules, not a
general guarantee that stored observations are true or commands deterministic.

## Rules shared by the typed boundaries

The envelope, result, manifest, input, conflict report, blob reference, shared
blob record and configuration must be objects. Their names are exact and
case-sensitive. Unknown names, repeated names (including escaped spellings of
the same name), extra JSON values and invalid UTF-8 bytes are rejected.
Each type's hook checks shape, decodes a temporary typed value, checks its values
and assigns the receiver only after success. No storage is performed by a hook.

Except for configuration, all declared fields must be present. Omission is not
equivalent to explicitly supplying zero. Wrong types are rejected. Null is
accepted only in the explicitly listed cases below. Command/configuration string
arrays cannot contain null members. Empty string arguments after the executable
are valid: they can be meaningful arguments to a command.

The standard Go decoder handles numbers, base64 output bytes and timestamps after
these shape checks. This is not a separate implementation of every JSON rule:
for example, the standard decoder's handling of escaped Unicode surrogate
sequences and base64 line breaks remains in use. We do not require field order,
whitespace or a single canonical spelling of equivalent string escapes.

## Fields and values

| Type | Required fields and value rules | Allowed nulls / compatibility |
| --- | --- | --- |
| Envelope | digest: lowercase 64-hex SHA-256; payload: a typed Result when Decode consumes it; digest must match the raw payload bytes | None |
| Result | version=1; key matches validated manifest; manifest; stdout/stderr: base64 strings; exit_code=0; duration_ns is a nonnegative int64; created_at is a Go-decodable timestamp | stdout/stderr may be null for nil streams; empty streams may also be empty strings; the zero timestamp is accepted |
| Manifest | version=1; root/directory/platform strings; command has a nonempty executable at index zero; inputs contain valid Input records, with no repeated kind/name pair | inputs may be null; empty context strings and an empty input list remain accepted; the server does not execute these paths or commands |
| Input | kind is file/environment/executable/toolchain; name is nonempty; hash is lowercase 64-hex SHA-256 | None |
| ConflictReport | key matches validated manifest; manifest; stdout_hash/stderr_hash are lowercase 64-hex SHA-256; exit_code is an integer from 0 through 255 | None; failed observations are reports, not reusable Results |
| BlobRef | hash is lowercase 64-hex SHA-256; size is an integer from 0 through 8 MiB | None |
| blobRecord | version=2; head is a valid Result with no inline output bytes; stdout/stderr are BlobRefs; storage readers also check the requested key and observation digest | None at record level; head's empty streams retain Result null rules |
| Config | extra_inputs and ignore_environment are string arrays; remote is a string; max_bytes is a nonnegative int64; all fields optional | Lists may be null; absent fields retain defaults; an explicit max_bytes=0 replaces the default |

Direct Go values passed to result/report storage are semantically validated too.
Presence and repeated JSON-name checks apply to serialized input, not Go values:
a Go struct has one value per declared field and cannot tell whether zero was
explicitly supplied by a caller.

## Size, checksum and side effects

Local envelope reads and HTTP request/response bodies retain the 32 MiB limit.
Result validation now enforces 8 MiB per output stream in local storage as well
as shared storage. Repository configuration reads are bounded at 1 MiB.
The shape helper does not impose a universal byte limit: direct json.Unmarshal
callers, previous-manifest history reads and database reads remain responsible
for their source size. Database records produced by Receipt come from bounded
writes; arbitrary operator-written database rows are not separately read-bounded.

A valid checksum does not excuse a malformed payload. Decode checks the envelope,
its payload digest and then the typed Result. It returns an error matching
cache.ErrCorrupt and an empty Result on failure. HTTP keeps generic 400 responses
for malformed entries/reports; local decode errors include field context. These
errors do not echo captured output or database credentials.

Incoming invalid HTTP entries/reports are rejected before Put/Report is called.
Invalid direct writes fail before publishing files or metadata. Shared readers
validate metadata before fetching output; collection validates all inspected
metadata before deleting any file. PostgreSQL JSONB has already erased duplicate
names from operator-inserted rows; read validation cannot reconstruct them.

## Compatibility and future changes

The v1 manifest/key encoding and local/HTTP envelope writer remain unchanged.
Valid records from the previous Go writer still decode, including nil output and
input slices and explicit zero metadata. Previously tolerated malformed records
are intentionally rejected. No automatic repair or rewrite of bad records occurs.
The compatibility tests preserve field order and digest relationships; they do
not establish interoperability with every client ever written.

Adding a field to a strict existing version breaks older readers. Plan a versioned
format/API or explicit migration/negotiation rather than silently loosening this
contract. A new input kind also needs a version/compatibility decision because
existing readers reject unknown kinds. Authentication and permission rules are
separate future work: validation checks shape and relationships, not who is
allowed to upload or whether their reported run really happened.
