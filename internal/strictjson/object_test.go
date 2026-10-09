package strictjson

import (
	"strings"
	"testing"
)

func TestObjectShape(t *testing.T) {
	tests := []struct {
		name, data, want string
	}{
		{"valid", `{"count":0,"items":[]}`, ""},
		{"reordered", ` { "items":null, "count":0 } `, ""},
		{"missing", `{"items":[]}`, "missing"},
		{"duplicate", `{"count":0,"count":1,"items":[]}`, "duplicate"},
		{"escaped duplicate", `{"count":0,"co\u0075nt":1,"items":[]}`, "duplicate"},
		{"wrong case", `{"Count":0,"items":[]}`, "unknown"},
		{"unknown", `{"count":0,"items":[],"other":true}`, "unknown"},
		{"null scalar", `{"count":null,"items":[]}`, "cannot be null"},
		{"null member", `{"count":0,"items":[null]}`, "not null"},
		{"wrong type", `{"count":"zero","items":[]}`, "cannot unmarshal"},
		{"array", `[]`, "expected JSON object"},
		{"root null", `null`, "expected JSON object"},
		{"trailing object", `{"count":0,"items":[]} {}`, "exactly one"},
		{"trailing garbage", `{"count":0,"items":[]} !`, "exactly one"},
		{"invalid UTF-8", "{\"count\":0,\"items\":[\"\xff\"]}", "UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dst struct {
				Count int      `json:"count"`
				Items []string `json:"items"`
			}
			err := Object([]byte(tt.data), &dst, Field{Name: "count"}, Field{Name: "items", Nullable: true, StringArray: true})
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q; got %v", tt.want, err)
			}
		})
	}
}

func TestOptionalFieldKeepsDefault(t *testing.T) {
	dst := struct {
		Count int `json:"count"`
	}{Count: 10}
	if err := Object([]byte(`{}`), &dst, Field{Name: "count", Optional: true}); err != nil || dst.Count != 10 {
		t.Fatal(dst, err)
	}
}
