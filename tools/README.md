# Repository tools

Run these commands from the Receipt checkout with Go 1.22 or later. They use
Go's standard library and are separate from the installed Receipt executable.

| Command | Purpose |
| --- | --- |
| `go run ./tools/docs sync` | Copy selected questions from the banks into chapters. |
| `make docs` | Build mdBook, test the checker, then check source and built pages. |
| `go run ./tools/artifacts` | Check tracked and nonignored working files. |
| `go run ./tools/artifacts --staged` | Check actual staged blobs, including sizes. |
| `go run ./tools/measure --repo "$PWD" --receipt ./receipt -- go test -count=1 ./internal/strictjson` | Compare direct runs, a miss and hits in a temporary cache. |
| `go run ./tools/release vX.Y.Z` | Build four local archives without publishing. |

`tools/selfci` is specific to the Linux GitHub runner and uses fixed temporary
paths. The workflow invokes it; ordinary tests do not run it against a real cache.
`tools/enroll` is an operator command that issues a server credential and updates
a GitHub secret. Read the [CI procedure](../docs/RECEIPT-CI.md#secrets-and-renewal)
before invoking it. Its tests substitute commands and never use live credentials.

`go test ./tools/...` tests the tools without requiring a built book, archived
measurements or a deployment. `make docs` also checks the complete built book.
Use ignored `scripts/utils/` for personal probes; shared workflows must not depend
on those files. Completed investigation tools remain available in Git history.
