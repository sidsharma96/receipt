package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type report struct {
	Purpose  string          `json:"purpose"`
	Reviewer string          `json:"reviewer"`
	Model    string          `json:"model"`
	Effort   string          `json:"effort"`
	Inputs   fingerprint     `json:"inputs"`
	Response json.RawMessage `json:"response"`
}

func verifyReport(r report, current fingerprint, o options) error {
	if o.purpose != purposeChange || r.Purpose != purposeChange {
		return errors.New("baseline/design reviews cannot approve a code change")
	}
	if r.Reviewer != o.reviewer || r.Model != model || r.Effort != effort {
		return errors.New("reviewer, model or effort differs from the requested configuration")
	}
	if r.Inputs != current {
		return errors.New("approval is stale: tree, base, evidence, role or catalog changed")
	}
	v, err := validResponse(r.Response)
	if err != nil {
		return err
	}
	if v.Verdict != verdictApprove {
		return errors.New("reviewer has not approved this change")
	}
	return nil
}

func verifyFile(path string, current fingerprint, o options) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var r report
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if err := verifyReport(r, current, o); err != nil {
		return err
	}
	fmt.Println("Approval matches this staged tree, base, evidence and local reviewer configuration. Check CI and private docs separately; publication still requires user authorization.")
	return nil
}

func writeReport(path string, r report) (resultErr error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	_, err = f.Write(append(data, '\n'))
	return err
}
