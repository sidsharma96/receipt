// Command enroll is an operator-only helper for the existing HTTPS CI credential.
// It must never run as part of CI, builds or tests.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	repository   = "sidsharma96/receipt"
	issueCommand = "sudo -u receipt env 'RECEIPT_DATABASE_URL=host=/var/run/postgresql user=receipt dbname=receipt sslmode=disable' /usr/local/bin/receipt auth issue --cache-dir /var/lib/receipt/cache --subject receipt-self-ci --scopes read,publish,report --expires-in 720h"
)

type command func([]byte, string, ...string) ([]byte, error)

func execute(input []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.Output()
	// Do not print subprocess stderr: issuance output contains a live secret.
	if err != nil {
		return nil, fmt.Errorf("%s command failed: %w", name, err)
	}
	return output, nil
}

func enroll(run command, key string, public io.Writer) error {
	data, err := run(nil, "gh", "secret", "list", "--repo", repository, "--json", "name")
	if err != nil {
		return err
	}
	var names []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &names); err != nil || names == nil {
		return fmt.Errorf("invalid secret listing")
	}
	for _, item := range names {
		if strings.HasPrefix(item.Name, "RECEIPT_SELF_CI_") {
			return fmt.Errorf("refusing to overwrite existing CI secrets")
		}
	}
	data, err = run(nil, "ssh", "-o", "BatchMode=yes", "-i", key, "sid@104.248.183.112", issueCommand)
	if err != nil {
		return err
	}
	return uploadCredential(run, data, public)
}

func uploadCredential(run command, data []byte, public io.Writer) error {
	var issued struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &issued); err != nil || issued.ID == "" || issued.Token == "" {
		return fmt.Errorf("invalid issuance reply; inspect server credentials before retrying")
	}
	// Output the public ID before upload so a failed upload can be revoked.
	if _, err := fmt.Fprintf(public, "Public credential ID: %s\n", issued.ID); err != nil {
		return fmt.Errorf("report credential ID: %w", err)
	}
	if _, err := run([]byte(issued.Token), "gh", "secret", "set", "RECEIPT_SELF_CI_TOKEN", "--repo", repository); err != nil {
		return fmt.Errorf("secret upload failed; revoke the reported credential before retrying: %w", err)
	}
	return nil
}

func main() {
	log.SetFlags(0)
	home, err := os.UserHomeDir()
	if err == nil {
		err = enroll(execute, filepath.Join(home, ".ssh", "receipt_do_ed25519"), os.Stdout)
	}
	if err != nil {
		log.Fatal(err)
	}
}
