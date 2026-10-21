package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/sidsharma96/receipt/internal/fingerprint"
)

func jsonBytesForTest(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// malformedObjects tests every required field, not just the successful zero exit code.
func malformedObjects(t *testing.T, data []byte, nullable map[string]bool) map[string][]byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"root null": []byte("null"), "root array": []byte("[]"),
		"extra object": append(bytes.Clone(data), []byte(` {}`)...),
		"unknown":      append([]byte(`{"unexpected":0,`), data[1:]...),
	}
	for name, value := range fields {
		delete(fields, name)
		cases["missing "+name] = jsonBytesForTest(t, fields)
		fields[strings.ToUpper(name)] = value
		cases["wrong case "+name] = jsonBytesForTest(t, fields)
		delete(fields, strings.ToUpper(name))
		if !nullable[name] {
			fields[name] = json.RawMessage("null")
			cases["null "+name] = jsonBytesForTest(t, fields)
		}
		fields[name] = value
		prefix := []byte(`{"` + name + `":` + string(value) + `,`)
		cases["duplicate "+name] = append(prefix, data[1:]...)
	}
	return cases
}

func TestTypedJSONBoundaries(t *testing.T) {
	v := example()
	record := blobRecord{Version: 2, Head: v, Stdout: BlobRef{fingerprint.Hash(v.Stdout), len(v.Stdout)}, Stderr: BlobRef{fingerprint.Hash(nil), 0}}
	record.Head.Stdout = nil
	values := []struct {
		name     string
		value    any
		nullable map[string]bool
	}{
		{"result", v, map[string]bool{"stdout": true, "stderr": true}},
		{"report", ReportFor(v), nil},
		{"reference", record.Stdout, nil},
		{"record", record, nil},
		{"envelope", envelope{fingerprint.Hash([]byte(`{}`)), json.RawMessage(`{}`)}, nil},
	}
	for _, tt := range values {
		t.Run(tt.name, func(t *testing.T) {
			data := jsonBytesForTest(t, tt.value)
			dst := reflect.New(reflect.TypeOf(tt.value))
			if err := json.Unmarshal(data, dst.Interface()); err != nil {
				t.Fatal("valid record rejected", err)
			}
			for name, bad := range malformedObjects(t, data, tt.nullable) {
				t.Run(name, func(t *testing.T) {
					if err := json.Unmarshal(bad, dst.Interface()); err == nil {
						t.Fatal("invalid record accepted", string(bad))
					}
					if !reflect.DeepEqual(dst.Elem().Interface(), tt.value) {
						t.Fatal("failed decode changed receiver")
					}
				})
			}
		})
	}
}

func checkedEnvelope(t *testing.T, payload []byte) []byte {
	t.Helper()
	return jsonBytesForTest(t, envelope{fingerprint.Hash(payload), payload})
}

func TestDecodeRejectsChecksummedMalformedPayload(t *testing.T) {
	for name, payload := range malformedObjects(t, jsonBytesForTest(t, example()), map[string]bool{"stdout": true, "stderr": true}) {
		// Trailing JSON cannot be inserted in a RawMessage by Marshal; test the
		// remaining malformed but syntactically valid payloads with a matching digest.
		if !json.Valid(payload) {
			continue
		}
		t.Run(name, func(t *testing.T) {
			got, err := Decode(checkedEnvelope(t, payload))
			if !errors.Is(err, ErrCorrupt) || !reflect.DeepEqual(got, Result{}) {
				t.Fatal("bad payload escaped validation", got, err)
			}
		})
	}
}

func TestSemanticResultAndReportRules(t *testing.T) {
	v := example()
	cases := map[string]Result{}
	r := v
	r.DurationNS = -1
	cases["negative duration"] = r
	r = v
	r.Manifest.Inputs = []fingerprint.Input{{Kind: "typo", Name: "x", Hash: fingerprint.Hash(nil)}}
	r.Key = r.Manifest.Key()
	cases["invalid input kind with matching key"] = r
	r = v
	r.Manifest.Command = nil
	r.Key = r.Manifest.Key()
	cases["empty command with matching key"] = r
	r = v
	r.Stdout = make([]byte, MaxBlobBytes+1)
	cases["oversize stream"] = r
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Validate(r); err == nil {
				t.Fatal("invalid direct result accepted")
			}
		})
	}
	for _, code := range []int{0, 1, 255} {
		report := ReportFor(v)
		report.ExitCode = code
		if err := json.Unmarshal(jsonBytesForTest(t, report), &report); err != nil {
			t.Fatal("valid observation exit code rejected", err)
		}
	}
	for _, code := range []int{-1, 256} {
		report := ReportFor(v)
		report.ExitCode = code
		if err := report.validate(); err == nil {
			t.Fatal("invalid exit code accepted")
		}
	}
}

func TestResultOutputRequiresBase64StringOrNull(t *testing.T) {
	data := string(jsonBytesForTest(t, example()))
	for _, value := range []string{`[112,97,115,115,10]`, `123`, `true`, `"not base64!"`} {
		bad := strings.Replace(data, `"stdout":"cGFzcwo="`, `"stdout":`+value, 1)
		var r Result
		if err := json.Unmarshal([]byte(bad), &r); err == nil {
			t.Fatal("non-wire output encoding accepted", value)
		}
	}
}

type boundarySpy struct{ writes, reports int }

func (*boundarySpy) Get(context.Context, string) (Result, error) { return Result{}, ErrMiss }
func (s *boundarySpy) Put(context.Context, Result) error         { s.writes++; return nil }
func (*boundarySpy) Quarantined(context.Context, string) (bool, error) {
	return false, nil
}
func (s *boundarySpy) Report(context.Context, ConflictReport) error { s.reports++; return nil }

func TestHTTPInvalidInputNeverCallsStorage(t *testing.T) {
	spy := &boundarySpy{}
	const token = "phase-four-test-token-long-enough"
	h := Handler(spy, token)
	v := example()
	for name, payload := range malformedObjects(t, jsonBytesForTest(t, v), map[string]bool{"stdout": true, "stderr": true}) {
		if !json.Valid(payload) {
			continue
		}
		t.Run("entry/"+name, func(t *testing.T) {
			assertBadRequest(t, h, http.MethodPut, "/v1/entries/"+v.Key, checkedEnvelope(t, payload), token)
		})
	}
	for name, payload := range malformedObjects(t, jsonBytesForTest(t, ReportFor(v)), nil) {
		t.Run("report/"+name, func(t *testing.T) {
			assertBadRequest(t, h, http.MethodPost, "/v1/conflicts/"+v.Key, payload, token)
		})
	}
	if spy.writes != 0 || spy.reports != 0 {
		t.Fatal("malformed input reached storage", spy)
	}
}

func assertBadRequest(t *testing.T, h http.Handler, method, path string, body []byte, token string) {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestVersionOneEncodingUnchanged(t *testing.T) {
	// An alias without decode hooks represents the original v1 writer.
	type legacyResult Result
	v := example()
	legacy := jsonBytesForTest(t, legacyResult(v))
	want := checkedEnvelope(t, legacy)
	got, err := Encode(v)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("wire bytes changed", err)
	}
	r, err := Decode(want)
	if err != nil || r.Key != v.Key || !bytes.Equal(r.Stdout, v.Stdout) || r.Stderr != nil {
		t.Fatal("legacy entry rejected", r, err)
	}
}

func FuzzDecode(f *testing.F) {
	b, err := Encode(example())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(b)
	f.Add([]byte(`{"digest":null,"payload":{}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		r, err := Decode(data)
		if err != nil {
			if !errors.Is(err, ErrCorrupt) || !reflect.DeepEqual(r, Result{}) {
				t.Fatal("invalid decode returned usable state", r, err)
			}
			return
		}
		encoded, err := Encode(r)
		if err != nil {
			t.Fatal("accepted result cannot be encoded", err)
		}
		again, err := Decode(encoded)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("round trip changed result", err)
		}
	})
}

func FuzzResultJSON(f *testing.F) {
	data, err := json.Marshal(example())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte(`{"exit_code":0,"exit_code":1}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		before := example()
		r := before
		if err := json.Unmarshal(data, &r); err != nil {
			if !reflect.DeepEqual(before, r) {
				t.Fatal("failed decode changed receiver")
			}
			return
		}
		if err := Validate(r); err != nil {
			t.Fatal("hook accepted invalid result", err)
		}
		encoded, err := Encode(r)
		if err != nil {
			// Direct json.Unmarshal has no entry byte budget; valid shapes can
			// serialize beyond the bounded envelope. Check only within that budget.
			return
		}
		again, err := Decode(encoded)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("accepted JSON cannot round trip", err)
		}
	})
}
