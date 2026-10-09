package fingerprint

import (
	"encoding/json"
	"testing"
)

func TestInputKindsPreserveVersionOneEncoding(t *testing.T) {
	m := Manifest{Version: 1, Root: "/repo", Directory: "/repo", Platform: "linux/amd64", Command: []string{"go", "test"}, Inputs: []Input{{FileInput, "a", "abc"}}}
	const legacy = `{"version":1,"root":"/repo","directory":"/repo","platform":"linux/amd64","command":["go","test"],"inputs":[{"kind":"file","name":"a","hash":"abc"}]}`
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != legacy || m.Key() != Hash([]byte(legacy)) {
		t.Fatalf("version-one encoding/key changed: %s", b)
	}
}
