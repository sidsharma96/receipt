package fingerprint

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManifestJSONRules(t *testing.T) {
	m := Manifest{Version: 1, Root: "/repo", Directory: "/repo", Platform: "linux/amd64", Command: []string{"pytest", ""}, Inputs: []Input{{FileInput, "a", Hash([]byte("a"))}}}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(b)
	cases := map[string]string{
		"unknown kind":       strings.Replace(valid, `"kind":"file"`, `"kind":"unknown"`, 1),
		"input null":         strings.Replace(valid, string(mustJSON(t, m.Inputs[0])), "null", 1),
		"input missing hash": strings.Replace(valid, `,"hash":"`+m.Inputs[0].Hash+`"`, "", 1),
		"input bad hash":     strings.Replace(valid, m.Inputs[0].Hash, "abc", 1),
		"input duplicate":    strings.Replace(valid, `"kind":"file"`, `"kind":"file","kind":"file"`, 1),
		"input unknown":      strings.Replace(valid, `"kind":"file"`, `"kind":"file","other":0`, 1),
		"null argument":      strings.Replace(valid, `"command":["pytest",""]`, `"command":["pytest",null]`, 1),
		"null command":       strings.Replace(valid, `"command":["pytest",""]`, `"command":null`, 1),
		"empty command":      strings.Replace(valid, `"command":["pytest",""]`, `"command":[]`, 1),
		"bad version":        strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"duplicate identity": strings.Replace(valid, `"inputs":[`, `"inputs":[`+string(mustJSON(t, m.Inputs[0]))+`,`, 1),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			before := Manifest{Version: 99}
			if err := json.Unmarshal([]byte(data), &before); err == nil {
				t.Fatal("accepted invalid manifest", data)
			}
			if before.Version != 99 {
				t.Fatal("failed decode changed receiver")
			}
		})
	}
	var got Manifest
	if err := json.Unmarshal(b, &got); err != nil || got.Key() != m.Key() {
		t.Fatal("valid manifest/key changed", err)
	}
	m.Inputs = nil
	if err := json.Unmarshal(mustJSON(t, m), &got); err != nil || got.Key() != m.Key() {
		t.Fatal("legacy null inputs/key changed", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
