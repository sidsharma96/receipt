package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedRefreshQuarantinesPriorSuccess(t *testing.T) {
	_, external := setup(t)
	state := filepath.Join(external, "state")
	os.WriteFile(state, []byte("yes"), 0o600)
	command := []string{"/bin/sh", "-c", "test $(cat '" + state + "') = yes"}
	invoke(t, append([]string{"run", "--allow-generic", "--"}, command...)...)
	os.WriteFile(state, []byte("no"), 0o600)
	code, _, status := invoke(t, append([]string{"run", "--refresh", "--allow-generic", "--"}, command...)...)
	if code != 1 || !strings.Contains(status, "quarantined") {
		t.Fatalf("%d %q", code, status)
	}
}
