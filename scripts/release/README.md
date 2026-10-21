# Publishing Receipt

Use an unused stable version such as `v0.1.0`. The application remains compatible
with Go 1.22; release binaries use the workflow's pinned Go 1.27.3 toolchain.

1. Run `make check`, `make race`, `make build`, `make docs` and the profile-helper
   tests. Review the staged source and artifact policy before committing.
2. Push the reviewed commit to main and wait for ordinary CI. Tag that exact
   commit with `git tag -a vX.Y.Z -m 'Receipt vX.Y.Z'`, then push that tag.
3. The Release workflow tests, builds four archives, checks the native Linux
   amd64 help command and uploads every asset into a draft before publication.
   Check the job and anonymously download the assets after publication.
4. Verify `go install github.com/sidsharma96/receipt/cmd/receipt@vX.Y.Z` from outside
   this checkout using the public proxy. Never move or replace a published tag.

For a local packaging check, run `python3 scripts/release/build.py vX.Y.Z` with
Go available. Outputs go under ignored `.tools/releases/vX.Y.Z`. The builder
refuses an existing destination; inspect any incomplete output before removing
that generated directory for a retry. The script builds but never publishes.

If an upload fails, inspect the existing draft and its attachments. Delete only
that incomplete draft before rerunning the job; do not delete the tag. If a
release is already published, fix source in a new version instead. The workflow
intentionally does not overwrite a published release or replace assets silently.

The Go proxy can see a tag before archive publication succeeds. These are
separate delivery routes. Download checksums are not signatures. Native Linux
arm64 and Intel macOS execution, Apple notarization, Windows binaries and
package-manager recipes are not covered by the initial release workflow.
