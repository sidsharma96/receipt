package app

import (
	"fmt"
	"slices"

	"github.com/sidsharma96/receipt/internal/strictjson"
)

// UnmarshalJSON preserves defaults for absent optional fields, while rejecting
// ambiguous names and null scalar settings. Null lists retain their v1 meaning.
func (c *Config) UnmarshalJSON(data []byte) error {
	type wire Config
	next := wire(*c)
	next.ExtraInputs = slices.Clone(next.ExtraInputs)
	next.IgnoreEnvironment = slices.Clone(next.IgnoreEnvironment)
	if err := strictjson.Object(data, &next,
		strictjson.Field{Name: "extra_inputs", Optional: true, Nullable: true, StringArray: true},
		strictjson.Field{Name: "ignore_environment", Optional: true, Nullable: true, StringArray: true},
		strictjson.Field{Name: "remote", Optional: true},
		strictjson.Field{Name: "max_bytes", Optional: true}); err != nil {
		return err
	}
	if next.MaxBytes < 0 {
		return fmt.Errorf("max_bytes must be nonnegative")
	}
	*c = Config(next)
	return nil
}
