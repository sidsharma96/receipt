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

This is the last part of that method, [`Result.UnmarshalJSON` in internal/cache/json.go](https://github.com/sidsharma96/receipt/blob/main/internal/cache/json.go#L11-L26):

```go
if err := Validate(Result(next)); err != nil {
	return err
}
*r = Result(next)
return nil
```

In the `if` line, `:=` creates the local error variable `err`; `!= nil` asks whether it contains an error. Earlier lines decode into a temporary value called `next`. `Validate` checks its meaning. If validation returns an error, the method returns without replacing the destination. `*r` refers to the result the caller asked the decoder to fill. Assignment happens only after the checks succeed. `nil` means there is no error.

Code that builds a result directly does not use JSON decoding, so storage methods also validate those values. A decoding hook is useful, but it is not permission checking and does not prove the sender ran the tests.

## What does a checksum tell us?

A stored envelope is a wrapper containing the result data and a checksum. The result data is its payload; the checksum is a hash calculated from those bytes. Reading it checks whether the payload still matches that hash. This can expose damage or accidental changes. Someone able to replace both the payload and checksum can still construct a matching pair.

## Read further

The exact fields, size limits, and compatibility rules belong in the [JSON input contract](../JSON-INPUT-CONTRACT.md). Publication and failure handling are explained in [Design reference](../DESIGN-DEFENSE.md).

## Check your understanding

<!-- questions:start -->

### Is a complete JSON document enough?

**Level:** Newcomer

A client uploads a result as JSON. Every required field is present and has the right type. Must the server accept it?

<details>
<summary>Answer</summary>

No. Receipt checks two separate things. The structure check rejects unknown fields, repeated fields and missing required fields. The value check then applies the rules about meaning: the version must be 1, the exit code must be 0, the key must equal the hash of the included manifest, and each output stream must fit the size limit. A well-formed document can still claim a failed run or carry a key that does not match its manifest, so the shape check does not replace the value check.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md) · [`Validate`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/store.go#L53-L73)

</details>

### Shape checks and value checks

**Level:** Practitioner

A JSON result has every required field and no unknown ones. Is that enough for Receipt to accept it? Where are the two kinds of check made?

<details>
<summary>Answer</summary>

No. A well-formed result can still claim a nonzero exit code, carry an invalid hash, or name a key that does not match its manifest. Structure and meaning are separate checks.

`Result.UnmarshalJSON` first calls `strictjson.Object`, which rejects invalid UTF-8, duplicate fields, unknown fields, missing required fields, wrongly cased names, disallowed nulls and trailing data, decoding into a temporary value. It then calls `Validate`, which requires version 1, a valid manifest, a key equal to the manifest's hash, exit code 0, a nonnegative duration and streams within 8 MiB. Only after both pass does it assign the caller's value. Putting this in the decoding hook means every path that decodes a result, from HTTP or from a local file, gets the checks without each caller remembering them. Storage methods call `Validate` again for results built in code.

**Learn more:** [Why check inside the type?](../guide/04-check-data-before-accepting-it.md#why-check-inside-the-type) · [JSON input contract](../JSON-INPUT-CONTRACT.md#rules-shared-by-the-typed-boundaries) · [`Result.UnmarshalJSON`](https://github.com/sidsharma96/receipt/blob/main/internal/cache/json.go#L9-L26) · [`Object`](https://github.com/sidsharma96/receipt/blob/main/internal/strictjson/object.go#L24-L61)

</details>

### A matching checksum on false data

**Level:** Expert

Someone can replace both a stored result and its checksum. They change the result and calculate a new matching checksum. Does checking the checksum prove the original tests passed?

<details>
<summary>Answer</summary>

No. The checksum establishes agreement between the stored bytes and the supplied hash. Someone who controls both can create that agreement. Validation can reject inconsistent or malformed data, but it does not prove that tests ran.

**Learn more:** [Chapter 4](../guide/04-check-data-before-accepting-it.md)

</details>

<!-- questions:end -->
