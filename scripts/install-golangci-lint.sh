#!/usr/bin/env bash
set -euo pipefail

# Install only a pinned, checksum-verified official binary inside the project.
lint_version="${1:-v2.14.0}"
lint_destination="${2:-.tools/bin/golangci-lint}"
if [[ ! "$lint_version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "Expected an exact golangci-lint version such as v2.14.0" >&2
  exit 1
fi
case "$(uname -s)" in
  Darwin) lint_os=darwin ;;
  Linux) lint_os=linux ;;
  *) echo "Install a supported golangci-lint binary manually" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  arm64|aarch64) lint_arch=arm64 ;;
  x86_64|amd64) lint_arch=amd64 ;;
  *) echo "Unsupported architecture" >&2; exit 1 ;;
esac
lint_temp=$(mktemp -d)
lint_name="golangci-lint-${lint_version#v}-${lint_os}-${lint_arch}"
lint_archive="$lint_name.tar.gz"
lint_checksums="golangci-lint-${lint_version#v}-checksums.txt"
cleanup_lint_download() {
  rm -f "$lint_temp/$lint_archive" "$lint_temp/$lint_checksums" "$lint_temp/$lint_name/golangci-lint"
  rmdir "$lint_temp/$lint_name" "$lint_temp" 2>/dev/null || true
}
trap cleanup_lint_download EXIT
lint_url="https://github.com/golangci/golangci-lint/releases/download/$lint_version"
curl --fail --location --retry 3 --silent --show-error "$lint_url/$lint_archive" -o "$lint_temp/$lint_archive"
curl --fail --location --retry 3 --silent --show-error "$lint_url/$lint_checksums" -o "$lint_temp/$lint_checksums"
lint_expected=$(awk -v name="$lint_archive" '$2 == name { print $1 }' "$lint_temp/$lint_checksums")
if command -v sha256sum >/dev/null; then
  lint_actual=$(sha256sum "$lint_temp/$lint_archive" | awk '{ print $1 }')
else
  lint_actual=$(shasum -a 256 "$lint_temp/$lint_archive" | awk '{ print $1 }')
fi
if [[ -z "$lint_expected" || "$lint_actual" != "$lint_expected" ]]; then
  echo "golangci-lint checksum mismatch" >&2
  exit 1
fi
tar -xzf "$lint_temp/$lint_archive" -C "$lint_temp" "$lint_name/golangci-lint"
mkdir -p "$(dirname "$lint_destination")"
cp "$lint_temp/$lint_name/golangci-lint" "$lint_destination"
"$lint_destination" version
