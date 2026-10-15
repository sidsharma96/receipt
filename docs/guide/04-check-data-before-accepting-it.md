# 4. Check data before accepting it

A client sends a result as [JSON](../GLOSSARY.md#json). What if it includes an unknown field, repeats a field, or claims that a failed run is reusable? Receipt needs to reject that data before treating it as a valid result.

There are two different questions. Does the document have the expected structure? And do its values make sense together?

For example, a result must name its key. That is a structural requirement. The key must also match its manifest, and reusable results must describe success. Those are rules about meaning.

**Figure 4. From incoming bytes to a stored result**

| Step | What happens |
|---|---|
| Bound the input | Receipt limits how much data it reads. |
| Check the JSON structure | It rejects unexpected, repeated, or missing required fields. |
| Check the values | It checks rules such as success and agreement between the key and manifest. |
| Accept the result | Only a valid result can proceed to storage. Stored envelopes also carry an integrity [checksum](../GLOSSARY.md#checksum). |

## Why check inside the type?

The same result type is read from HTTP requests and local files. Putting its decoding checks in `UnmarshalJSON` makes them run when Go's JSON decoder fills that type, rather than relying on each caller to remember them.

This is the last part of that method, [`Result.UnmarshalJSON` in internal/cache/json.go](https://github.com/sidsharma96/receipt/blob/main/internal/cache/json.go#L10-L25):

```go
if err := Validate(Result(next)); err != nil {
	return err
}
*r = Result(next)
return nil
```

Earlier lines decode into a temporary value called `next`. `Validate` checks its meaning. If validation returns an error, the method returns without replacing the destination. `*r` refers to the result the caller asked the decoder to fill. Assignment happens only after the checks succeed. `nil` means there is no error.

Code that builds a result directly does not use JSON decoding, so storage methods also validate those values. A decoding hook is useful, but it is not permission checking and does not prove the sender ran the tests.

## What does a checksum tell us?

A stored envelope includes a hash of its payload. Reading it checks whether the payload still matches that hash. This can expose damage or accidental changes. Someone able to replace both the payload and checksum can still construct a matching pair.

## Read further

The exact fields, size limits, and compatibility rules belong in the [JSON input contract](../JSON-INPUT-CONTRACT.md). Interrupted-write evidence is in [Phase 4's review](../PHASE-REVIEWS.md#phase-4-review-checking-incoming-data-and-interrupted-writes).

## Check your understanding

<!-- questions:start -->

### Is a complete JSON document enough?

**Level:** Newcomer

A client uploads a result as JSON. Every required field is present and has the right type. Must the server accept it?

<details>
<summary>Answer</summary>

No. Receipt checks two separate things. The structure check rejects unknown fields, repeated fields and missing required fields. The value check then applies the rules about meaning: the version must be 1, the exit code must be 0, the key must equal the hash of the included manifest, and each output stream must fit the size limit. A well-formed document can still claim a failed run or carry a key that does not match its manifest, so the shape check does not replace the value check.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L52-L72)

</details>

### Shape checks and value checks

**Level:** Practitioner

A JSON result has every required field and no unknown ones. Is that enough for Receipt to accept it? Where are the two kinds of check made?

<details>
<summary>Answer</summary>

No. A well-formed result can still claim a nonzero exit code, carry an invalid hash, or name a key that does not match its manifest. Structure and meaning are separate checks.

`Result.UnmarshalJSON` first calls `strictjson.Object`, which rejects invalid UTF-8, duplicate fields, unknown fields, missing required fields, wrongly cased names, disallowed nulls and trailing data, decoding into a temporary value. It then calls `Validate`, which requires version 1, a valid manifest, a key equal to the manifest's hash, exit code 0, a nonnegative duration and streams within 8 MiB. Only after both pass does it assign the caller's value. Putting this in the decoding hook means every path that decodes a result, from HTTP or from a local file, gets the checks without each caller remembering them. Storage methods call `Validate` again for results built in code.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md#rules-shared-by-the-typed-boundaries) · [`Result.UnmarshalJSON`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/json.go#L8-L25) · [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61)

</details>

### Poisoning with a publish credential

**Level:** Expert

An attacker holds a `publish`-scoped credential. What can they do to other users' test results, what limits the damage, and which CI rule exists because of this?

<details>
<summary>Answer</summary>

They can forge a passing result. The key is the hash of a manifest, so anyone who can reconstruct a victim's manifest can compute the key and upload a result with exit code 0 and any stdout they like. The server checks structure and consistency: strict JSON, size limits, the checksum, that the key matches the manifest and that the exit code is 0. It cannot tell whether tests ran. If nobody has stored that key yet, the forgery is accepted and every client that later misses locally fetches and replays a fake pass. The first writer wins, so a forgery placed early sticks.

Detection comes only from disagreement: an honest upload with different output for the same key, or a `--verify` run, quarantines it. Scopes let most callers hold `read` only. CI never gives private-cache credentials to pull-request code: the cached job runs only on pushes to trusted branches, and an independent uncached job always runs the full suite, so CI correctness never depends on the cache.

**Learn more:** [Part 3. Threat and failure model](../DESIGN-DEFENSE.md#part-3-threat-and-failure-model) · [Why keep the uncached job?](../RECEIPT-CI.md#why-keep-the-uncached-job) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L51-L73)

</details>

<!-- questions:end -->
