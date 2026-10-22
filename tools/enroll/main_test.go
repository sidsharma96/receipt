package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRefusesExistingSecretsBeforeIssuing(t *testing.T) {
	calls := 0
	fake := func(_ []byte, _ string, _ ...string) ([]byte, error) {
		calls++
		return []byte(`[{"name":"RECEIPT_SELF_CI_TOKEN"}]`), nil
	}
	if err := enroll(fake, "unused", &bytes.Buffer{}); err == nil || calls != 1 {
		t.Fatal("issued despite existing secret")
	}
}

func TestUploadKeepsTokenOffArgumentsAndOutput(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var output bytes.Buffer
		fake := func(input []byte, name string, args ...string) ([]byte, error) {
			if string(input) != "private-token" || name != "gh" || strings.Contains(strings.Join(args, " "), "private-token") {
				t.Fatal("incorrect secret transport")
			}
			if !strings.Contains(output.String(), "public-id") {
				t.Fatal("ID not reported before upload")
			}
			if fail {
				return nil, errors.New("upload failed")
			}
			return nil, nil
		}
		err := uploadCredential(fake, []byte(`{"id":"public-id","token":"private-token"}`), &output)
		if (err != nil) != fail || strings.Contains(output.String(), "private-token") {
			t.Fatal("incorrect failure/output policy")
		}
	}
}
