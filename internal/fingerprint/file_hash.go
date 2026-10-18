package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// hashFile hashes prefix followed by every file byte, using at most 32 KiB of
// scratch space. The size hint only sizes the buffer; it never limits the read.
func hashFile(path, prefix string, size int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // Read-only cleanup; preserve any read error.
	h := sha256.New()
	if _, err := io.WriteString(h, prefix); err != nil {
		return "", err
	}
	buffer := make([]byte, min(max(size, 1), 32<<10))
	// Hide File.WriteTo so io.CopyBuffer uses this bounded, size-aware buffer.
	if _, err := io.CopyBuffer(h, struct{ io.Reader }{f}, buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
