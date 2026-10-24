// Command review runs a read-only Claude review of the staged source tree.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"github.com/sidsharma96/receipt/tools/internal/toolutil"
)

const (
	model          = "claude-opus-5-5"
	effort         = "low"
	catalogPath    = "review/upstream.json"
	purposeChange  = "change"
	verdictApprove = "APPROVE"
)

type options struct {
	action, reviewer, base, purpose, evidence, report string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseOptions(args []string) (options, error) {
	var o options
	if len(args) == 0 || (args[0] != "run" && args[0] != "verify") {
		return o, errors.New("usage: review run|verify --reviewer code|architecture --evidence FILE --report FILE [--base origin/main] [--purpose change|design|baseline]")
	}
	o.action = args[0]
	f := flag.NewFlagSet("review", flag.ContinueOnError)
	f.StringVar(&o.reviewer, "reviewer", "", "code or architecture (required)")
	f.StringVar(&o.base, "base", "origin/main", "comparison commit")
	f.StringVar(&o.purpose, "purpose", purposeChange, "change, design or baseline")
	f.StringVar(&o.evidence, "evidence", "", "check results and scope (required)")
	f.StringVar(&o.report, "report", "", "new ignored report path (required)")
	if err := f.Parse(args[1:]); err != nil {
		return o, err
	}
	if f.NArg() != 0 || (o.reviewer != "code" && o.reviewer != "architecture") || o.evidence == "" || o.report == "" {
		return o, errors.New("specify a reviewer, evidence and report; no positional arguments are accepted")
	}
	if o.purpose != purposeChange && o.purpose != "design" && o.purpose != "baseline" {
		return o, errors.New("purpose must be change, design or baseline")
	}
	return o, nil
}

func run(args []string) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	root, err := toolutil.Root()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	ctx, timeout := context.WithTimeout(ctx, 15*time.Minute)
	defer timeout()
	current, inputs, err := capture(ctx, root, o)
	if err != nil {
		return err
	}
	if o.action == "verify" {
		return verifyFile(o.report, current, o)
	}
	if err := reportDestination(ctx, root, o.report); err != nil {
		return err
	}
	raw, err := invoke(ctx, root, current, inputs, o)
	if err != nil {
		return err
	}
	after, _, err := capture(ctx, root, o)
	if err != nil {
		return err
	}
	if current != after {
		return errors.New("source, base, role, catalog or evidence changed during review; run again")
	}
	r := report{Purpose: o.purpose, Reviewer: o.reviewer, Model: model, Effort: effort, Inputs: current, Response: raw}
	if err := writeReport(o.report, r); err != nil {
		return err
	}
	v, err := validResponse(raw)
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\nReport: %s\n", v.Verdict, v.Summary, o.report)
	return nil
}

func reportDestination(ctx context.Context, root, name string) error {
	path, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("use a new report path; existing reports are never overwritten")
	}
	if _, err := git(ctx, root, "check-ignore", "--", path); err != nil {
		return errors.New("report must be inside an ignored directory such as .tools/reviews")
	}
	return nil
}
