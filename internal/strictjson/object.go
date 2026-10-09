// Package strictjson checks JSON object shape before typed decoding.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// Field declares an exact JSON name and its presence/null policy.
type Field struct {
	Name     string
	Optional bool
	Nullable bool
	// String requires string encoding (rather than another JSON representation).
	String bool
	// StringArray rejects null members as well as non-string members.
	StringArray bool
}

// Object rejects duplicate, unknown, missing, wrongly cased and disallowed null
// fields. Callers must bound data first and decode into a temporary value: dst
// may be partly written on a type error. Nested objects need their own hooks.
func Object(data []byte, dst any, fields ...Field) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("JSON must be UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	allowed := make(map[string]Field, len(fields))
	for _, field := range fields {
		allowed[field.Name] = field
	}
	seen := make(map[string]bool, len(fields))
	for d.More() {
		if err := readField(d, allowed, seen); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected exactly one JSON object")
	}
	for _, field := range fields {
		if !field.Optional && !seen[field.Name] {
			return fmt.Errorf("missing field %q", field.Name)
		}
	}
	return json.Unmarshal(data, dst)
}

func readField(d *json.Decoder, allowed map[string]Field, seen map[string]bool) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	name, ok := token.(string)
	if !ok {
		return fmt.Errorf("expected field name")
	}
	field, ok := allowed[name]
	if !ok {
		return fmt.Errorf("unknown field %q", name)
	}
	if seen[name] {
		return fmt.Errorf("duplicate field %q", name)
	}
	seen[name] = true
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil {
		return fmt.Errorf("field %q: %w", name, err)
	}
	isNull := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	if !field.Nullable && isNull {
		return fmt.Errorf("field %q cannot be null", name)
	}
	if field.String && !isNull {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	if field.StringArray {
		if err := stringArray(raw); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}

func stringArray(raw []byte) error {
	var items []*string
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("item %d must be a string, not null", i)
		}
	}
	return nil
}
