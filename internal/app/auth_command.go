package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"receipt/internal/auth"
	"time"
)

type authMode string

const (
	authIssue  authMode = "issue"
	authList   authMode = "list"
	authRevoke authMode = "revoke"
)

func parseAuthMode(raw string) (authMode, error) {
	mode := authMode(raw)
	switch mode {
	case authIssue, authList, authRevoke:
		return mode, nil
	default:
		return "", fmt.Errorf("auth requires issue, list or revoke")
	}
}

func authCommand(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("auth requires issue, list or revoke")
	}
	mode, err := parseAuthMode(args[0])
	if err != nil {
		return err
	}
	f := flag.NewFlagSet("auth "+string(mode), flag.ContinueOnError)
	f.SetOutput(errOut)
	dir := f.String("cache-dir", "", "shared server cache directory (required)")
	subject := f.String("subject", "", "operator-assigned caller label (issue only)")
	rawScopes := f.String("scopes", "", "explicit comma-separated read,publish,report,metrics (issue only)")
	lifetime := f.Duration("expires-in", 24*time.Hour, "credential lifetime from 1s to 720h (issue only)")
	id := f.String("id", "", "credential id to revoke (revoke only)")
	timeout := f.Duration("timeout", 15*time.Second, "administration timeout")
	if err := f.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(f.Args()) != 0 || *dir == "" || *timeout <= 0 {
		return fmt.Errorf("explicit --cache-dir and positive timeout required; no positional arguments")
	}
	if err := checkAuthFlags(f, mode); err != nil {
		return err
	}
	var scopes []auth.Scope
	if mode == authIssue {
		scopes, err = auth.ParseScopes(*rawScopes)
		if err != nil {
			return err
		}
		if err := auth.ValidateIssue(*subject, scopes, *lifetime); err != nil {
			return err
		}
	}
	if mode == authRevoke {
		if err := auth.ValidateID(*id); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	shared, err := openShared(ctx, *dir)
	if err != nil {
		return err
	}
	defer shared.DB.Close() //nolint:errcheck // The command owns the pool; cleanup must not replace its primary error.
	credentials := auth.Postgres{DB: shared.DB}
	if err := credentials.Migrate(ctx); err != nil {
		return fmt.Errorf("credential initialization failed: %w", err)
	}
	return executeAuth(ctx, mode, credentials, *subject, scopes, *lifetime, *id, out)
}

func checkAuthFlags(f *flag.FlagSet, mode authMode) error {
	visited := map[string]bool{}
	f.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	for _, field := range []struct {
		name string
		mode authMode
	}{{"subject", authIssue}, {"scopes", authIssue}, {"expires-in", authIssue}, {"id", authRevoke}} {
		if visited[field.name] && mode != field.mode {
			return fmt.Errorf("%s is only for auth %s", field.name, field.mode)
		}
	}
	return nil
}

func executeAuth(ctx context.Context, mode authMode, credentials auth.Postgres, subject string, scopes []auth.Scope, lifetime time.Duration, id string, out io.Writer) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	switch mode {
	case authIssue:
		issued, err := credentials.Issue(ctx, subject, scopes, lifetime)
		if err != nil {
			return err
		}
		// A broken output stream cannot roll back an already committed credential.
		// List/revoke its public id and issue a replacement if the secret was lost.
		return encoder.Encode(issued)
	case authList:
		list, err := credentials.List(ctx)
		if err != nil {
			return err
		}
		return encoder.Encode(list)
	case authRevoke:
		if err := credentials.Revoke(ctx, id); err != nil {
			return err
		}
		return encoder.Encode(struct {
			ID      string `json:"id"`
			Revoked bool   `json:"revoked"`
		}{id, true})
	}
	return nil
}
