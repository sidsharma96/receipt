Receipt reuses successful test results when declared inputs match.

Install with Go:

```sh
go install github.com/sidsharma96/receipt/cmd/receipt@latest
```

Or download the archive for your OS and CPU and verify it against SHA256SUMS.
`darwin` means macOS; `arm64` includes Apple Silicon; `amd64` means Intel/AMD 64-bit.
Archives include the executable, README, Apache 2.0 license and dependency notices.
Git and your workload's tools remain required. Local use needs no cloud account.

This is an early 0.x release. See the README for the declared-input contract and
unsupported inputs. Windows binaries, an automatic updater and Apple notarization
are not included. Go builds carry module/build information (`go version -m receipt`).
Checksums detect changed downloads; they are not a separate publisher signature.
