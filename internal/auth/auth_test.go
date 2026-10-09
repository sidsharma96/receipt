package auth

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestScopeParsingAndNoImplicitPermissions(t *testing.T) {
	for _, raw := range []string{"", "READ", "read,read", "read,", "read, publish", "admin", "*"} {
		if _, err := ParseScopes(raw); err == nil {
			t.Fatal("invalid scopes accepted", raw)
		}
	}
	scopes, err := ParseScopes("report,publish,read,metrics")
	if err != nil || !reflect.DeepEqual(scopes, []Scope{Metrics, Publish, Read, Report}) {
		t.Fatal(scopes, err)
	}
	p := Principal{Scopes: []Scope{Publish}}
	if !p.Allows(Publish) || p.Allows(Read) || p.Allows(Report) || p.Allows(Metrics) || p.Allows("unknown") {
		t.Fatal("permissions implied unexpectedly")
	}
}

func TestTokenFormatAndUniquenessSample(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, token, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := tokenID(token)
		if err != nil || parsed != id || len(token) != TokenLength || seen[token] || digest(token) == token {
			t.Fatal("invalid or repeated generated token")
		}
		seen[token] = true
		for _, bad := range []string{"", id, digest(token), strings.ToUpper(token), token + " ", strings.Replace(token, "rcpt_", "other", 1)} {
			if _, err := tokenID(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("bad token format accepted")
			}
		}
	}
	// Uniqueness in this small sample is not a statistical entropy proof.
}

func TestInvalidIssueRejectedBeforeDatabaseAccess(t *testing.T) {
	for _, tc := range []struct {
		subject string
		scopes  []Scope
		ttl     time.Duration
	}{
		{"", []Scope{Read}, time.Hour},
		{"alice\n", []Scope{Read}, time.Hour},
		{strings.Repeat("a", 129), []Scope{Read}, time.Hour},
		{"alice", nil, time.Hour},
		{"alice", []Scope{"bad"}, time.Hour},
		{"alice", []Scope{Read, Read}, time.Hour},
		{"alice", []Scope{Read}, 0},
		{"alice", []Scope{Read}, time.Millisecond},
		{"alice", []Scope{Read}, MaxLifetime + time.Second},
	} {
		if issued, err := (Postgres{}).Issue(context.Background(), tc.subject, tc.scopes, tc.ttl); err == nil || issued.Token != "" {
			t.Fatal("invalid issue reached database or exposed token")
		}
	}
}

func TestPublicCredentialJSONExcludesHashAndSecret(t *testing.T) {
	b, err := json.Marshal(Credential{Principal: Principal{ID: "id", Subject: "alice", Scopes: []Scope{Read}}})
	if err != nil || strings.Contains(string(b), "token") || strings.Contains(string(b), "hash") {
		t.Fatal("public credential metadata contains secret fields", err)
	}
}

func TestIssueValidationAggregatesIndependentFields(t *testing.T) {
	err := ValidateIssue("", nil, 0)
	if err == nil {
		t.Fatal("invalid fields accepted")
	}
	for _, field := range []string{"subject", "scopes", "expires-in"} {
		if !strings.Contains(err.Error(), field) {
			t.Fatal("missing independent validation error", field)
		}
	}
}

func FuzzTokenID(f *testing.F) {
	f.Add("rcpt_" + strings.Repeat("a", 32) + "_" + strings.Repeat("b", 64))
	f.Add("")
	f.Fuzz(func(t *testing.T, token string) {
		id, err := tokenID(token)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || id != "" {
				t.Fatal("invalid parser failure state")
			}
			return
		}
		if len(token) != TokenLength || !idPattern.MatchString(id) || !strings.HasPrefix(token, "rcpt_"+id+"_") {
			t.Fatal("accepted malformed token")
		}
	})
}
