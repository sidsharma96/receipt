// Package auth implements bounded, database-backed service credentials.
// A subject is an operator-assigned label, not proof of a person's identity.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Scope identifies one permitted API operation group.
type Scope string

const (
	// Read allows result lookup and quarantine-status checks.
	Read Scope = "read"
	// Publish allows successful-result uploads (which may detect conflicts).
	Publish Scope = "publish"
	// Report allows explicit conflict reports that can quarantine a shared key.
	Report Scope = "report"
	// Metrics allows aggregate operational metrics.
	Metrics Scope = "metrics"
)

var (
	// ErrInvalid indicates an absent, expired, revoked or invalid credential.
	ErrInvalid = errors.New("invalid credential")
	// ErrCapacity indicates the retained credential-row budget is full.
	ErrCapacity    = errors.New("credential capacity reached")
	idPattern      = regexp.MustCompile(`^[a-f0-9]{32}$`)
	subjectPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:@/-]{0,127}$`)
	tokenPattern   = regexp.MustCompile(`^rcpt_([a-f0-9]{32})_([a-f0-9]{64})$`)
)

const (
	// TokenLength is the exact length of a generated ASCII bearer token.
	TokenLength = 102
	// MaxLifetime bounds issued credentials to thirty days.
	MaxLifetime = 30 * 24 * time.Hour
	// MaxCredentials bounds all retained rows, including expired/revoked ones.
	MaxCredentials = 1024
)

// Principal is the identity/permission snapshot for one authenticated request.
type Principal struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Scopes    []Scope   `json:"scopes"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Allows checks one explicit scope. No scope implies another scope.
func (p Principal) Allows(scope Scope) bool { return slices.Contains(p.Scopes, scope) }

// Authenticator checks a credential at request time. It must not return a
// principal on error; callers must not authorize a failed authentication.
type Authenticator interface {
	Authenticate(context.Context, string) (Principal, error)
}

// Credential exposes administration metadata, never the stored token hash.
type Credential struct {
	Principal
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Issued reveals a generated bearer token once, after successful persistence.
type Issued struct {
	Credential
	Token string `json:"token"`
}

// ParseScopes rejects unknown, duplicate and empty choices; it canonicalizes order.
func ParseScopes(raw string) ([]Scope, error) {
	parts := strings.Split(raw, ",")
	scopes := make([]Scope, len(parts))
	for i, part := range parts {
		scopes[i] = Scope(part)
	}
	if err := validateScopes(scopes); err != nil {
		return nil, err
	}
	slices.Sort(scopes)
	return scopes, nil
}

func validateScopes(scopes []Scope) error {
	if len(scopes) == 0 || len(scopes) > 4 {
		return fmt.Errorf("one to four explicit scopes required")
	}
	seen := map[Scope]bool{}
	for _, scope := range scopes {
		switch scope {
		case Read, Publish, Report, Metrics:
		default:
			return fmt.Errorf("unknown scope %q", scope)
		}
		if seen[scope] {
			return fmt.Errorf("duplicate scope %q", scope)
		}
		seen[scope] = true
	}
	return nil
}

// ValidateIssue checks administration input before any database access.
func ValidateIssue(subject string, scopes []Scope, lifetime time.Duration) error {
	var errs []error
	if !subjectPattern.MatchString(subject) {
		errs = append(errs, fmt.Errorf("subject must be 1–128 ASCII label characters (letters, digits, . _ : @ / -)"))
	}
	if err := validateScopes(scopes); err != nil {
		errs = append(errs, fmt.Errorf("scopes: %w", err))
	}
	if lifetime < time.Second || lifetime > MaxLifetime {
		errs = append(errs, fmt.Errorf("expires-in must be between 1s and 720h"))
	}
	return errors.Join(errs...)
}

// ValidateID checks the public identifier before administration database access.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid credential id")
	}
	return nil
}

func generate() (id, token string, err error) {
	var data [48]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", "", fmt.Errorf("generate credential: %w", err)
	}
	id = hex.EncodeToString(data[:16])
	return id, "rcpt_" + id + "_" + hex.EncodeToString(data[16:]), nil
}

func tokenID(token string) (string, error) {
	// Reject long attacker input before running the regexp or accessing the DB.
	if len(token) != TokenLength {
		return "", ErrInvalid
	}
	parts := tokenPattern.FindStringSubmatch(token)
	if len(parts) != 3 {
		return "", ErrInvalid
	}
	return parts[1], nil
}

func digest(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
