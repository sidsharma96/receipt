package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigJSONRules(t *testing.T) {
	for _, data := range []string{
		`null`, `[]`, `{"Max_Bytes":0}`, `{"max_bytes":0,"max_bytes":1}`,
		`{"max_bytes":null}`, `{"remote":null}`, `{"extra_inputs":[null]}`,
		`{"ignore_environment":[0]}`, `{"max_bytes":-1}`, `{"typo":true}`, `{} {}`,
	} {
		t.Run(data, func(t *testing.T) {
			cfg := Config{MaxBytes: 1024}
			if err := json.Unmarshal([]byte(data), &cfg); err == nil {
				t.Fatal("accepted invalid configuration")
			}
			if cfg.MaxBytes != 1024 {
				t.Fatal("failed decode changed defaults")
			}
		})
	}
	for _, data := range []string{`{}`, `{"extra_inputs":null,"ignore_environment":[]}`, `{"remote":""}`} {
		cfg := Config{MaxBytes: 1024}
		if err := json.Unmarshal([]byte(data), &cfg); err != nil || cfg.MaxBytes != 1024 {
			t.Fatal(cfg, err)
		}
	}
	cfg := Config{MaxBytes: 1024}
	if err := json.Unmarshal([]byte(`{"max_bytes":0}`), &cfg); err != nil || cfg.MaxBytes != 0 {
		t.Fatal("explicit zero did not replace default", cfg, err)
	}
}

func TestSettingsBoundsConfigAndRejectsAmbiguity(t *testing.T) {
	dir, _ := setup(t)
	for _, data := range []string{strings.Repeat(" ", (1<<20)+1), `{"max_bytes":0,"max_bytes":1}`} {
		if err := os.WriteFile(filepath.Join(dir, ".receipt.json"), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := settings(context.Background(), dir); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}

func TestConfigFailedDecodePreservesExistingSlices(t *testing.T) {
	cfg := Config{MaxBytes: 1024, ExtraInputs: []string{"original"}}
	if err := json.Unmarshal([]byte(`{"extra_inputs":["changed"],"max_bytes":"bad"}`), &cfg); err == nil {
		t.Fatal("invalid config accepted")
	}
	if cfg.MaxBytes != 1024 || !reflect.DeepEqual(cfg.ExtraInputs, []string{"original"}) {
		t.Fatal("failed decode mutated existing settings", cfg)
	}
}
