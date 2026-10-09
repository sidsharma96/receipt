package cache

import (
	"fmt"
	"receipt/internal/strictjson"
)

// UnmarshalJSON checks the result schema before assigning the receiver. Only
// stdout/stderr may be null: v1 writers encode nil byte slices this way.
func (r *Result) UnmarshalJSON(data []byte) error {
	type wire Result
	var next wire
	if err := strictjson.Object(data, &next,
		strictjson.Field{Name: "version"}, strictjson.Field{Name: "key"},
		strictjson.Field{Name: "manifest"}, strictjson.Field{Name: "stdout", Nullable: true, String: true},
		strictjson.Field{Name: "stderr", Nullable: true, String: true}, strictjson.Field{Name: "exit_code"},
		strictjson.Field{Name: "duration_ns"}, strictjson.Field{Name: "created_at"}); err != nil {
		return fmt.Errorf("result: %w", err)
	}
	if err := Validate(Result(next)); err != nil {
		return err
	}
	*r = Result(next)
	return nil
}

func (e *envelope) UnmarshalJSON(data []byte) error {
	type wire envelope
	var next wire
	if err := strictjson.Object(data, &next, strictjson.Field{Name: "digest"}, strictjson.Field{Name: "payload"}); err != nil {
		return err
	}
	if !validKey.MatchString(next.Digest) {
		return fmt.Errorf("envelope.digest: expected lowercase SHA-256")
	}
	*e = envelope(next)
	return nil
}

// UnmarshalJSON validates reports at every typed decoding boundary, not just HTTP.
func (r *ConflictReport) UnmarshalJSON(data []byte) error {
	type wire ConflictReport
	var next wire
	if err := strictjson.Object(data, &next,
		strictjson.Field{Name: "key"}, strictjson.Field{Name: "manifest"},
		strictjson.Field{Name: "stdout_hash"}, strictjson.Field{Name: "stderr_hash"},
		strictjson.Field{Name: "exit_code"}); err != nil {
		return fmt.Errorf("report: %w", err)
	}
	if err := ConflictReport(next).validate(); err != nil {
		return err
	}
	*r = ConflictReport(next)
	return nil
}

// UnmarshalJSON checks references before a reader opens any output files.
func (r *BlobRef) UnmarshalJSON(data []byte) error {
	type wire BlobRef
	var next wire
	if err := strictjson.Object(data, &next, strictjson.Field{Name: "hash"}, strictjson.Field{Name: "size"}); err != nil {
		return fmt.Errorf("blob reference: %w", err)
	}
	if !validKey.MatchString(next.Hash) || next.Size < 0 || next.Size > MaxBlobBytes {
		return fmt.Errorf("invalid blob reference")
	}
	*r = BlobRef(next)
	return nil
}

func (r *blobRecord) UnmarshalJSON(data []byte) error {
	type wire blobRecord
	var next wire
	if err := strictjson.Object(data, &next,
		strictjson.Field{Name: "version"}, strictjson.Field{Name: "head"},
		strictjson.Field{Name: "stdout"}, strictjson.Field{Name: "stderr"}); err != nil {
		return err
	}
	if next.Version != 2 || len(next.Head.Stdout) != 0 || len(next.Head.Stderr) != 0 {
		return fmt.Errorf("invalid blob record")
	}
	*r = blobRecord(next)
	return nil
}
