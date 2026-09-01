package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/UnbalancedCat/ipgw-meta/internal/candidate"
)

type commandResult struct {
	CandidateID        string `json:"candidate_id"`
	CandidateSetSHA256 string `json:"candidate_set_sha256"`
	BuildInputSHA256   string `json:"build_input_sha256"`
	SourceCommit       string `json:"source_commit"`
	SourceTree         string `json:"source_tree"`
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if ctx == nil || stdout == nil || stderr == nil || len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "ipgw-candidate: invalid invocation")
		return 2
	}
	var result candidate.Result
	var err error
	switch args[0] {
	case "assemble":
		result, err = runAssemble(ctx, args[1:])
	case "verify":
		result, err = runVerify(args[1:])
	default:
		_, _ = fmt.Fprintln(stderr, "ipgw-candidate: invalid invocation")
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ipgw-candidate: operation failed")
		return 1
	}
	output := commandResult{
		CandidateID: result.CandidateID, CandidateSetSHA256: result.CandidateSetSHA256,
		BuildInputSHA256: result.BuildInputSHA256, SourceCommit: result.SourceCommit, SourceTree: result.SourceTree,
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(true)
	if encoder.Encode(output) != nil {
		_, _ = fmt.Fprintln(stderr, "ipgw-candidate: output failed")
		return 1
	}
	return 0
}

func runAssemble(ctx context.Context, args []string) (candidate.Result, error) {
	flags := flag.NewFlagSet("assemble", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var options candidate.AssembleOptions
	flags.StringVar(&options.RepositoryRoot, "repository", "", "")
	flags.StringVar(&options.SourceCommit, "source-commit", "", "")
	flags.StringVar(&options.CandidateID, "candidate-id", "", "")
	flags.Int64Var(&options.WorkflowRunID, "workflow-run-id", 0, "")
	flags.Int64Var(&options.WorkflowRunAttempt, "workflow-run-attempt", 0, "")
	flags.StringVar(&options.BuildDir, "build-dir", "", "")
	flags.StringVar(&options.OutputDir, "output-dir", "", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return candidate.Result{}, candidate.ErrInvalidInput
	}
	return candidate.Assemble(ctx, options)
}

func runVerify(args []string) (candidate.Result, error) {
	flags := flag.NewFlagSet("verify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var root string
	flags.StringVar(&root, "candidate-set", "", "")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return candidate.Result{}, candidate.ErrInvalidInput
	}
	normalized, ok := normalizeVerifyRoot(root)
	if !ok {
		return candidate.Result{}, candidate.ErrInvalidInput
	}
	return candidate.Verify(normalized)
}

func normalizeVerifyRoot(root string) (string, bool) {
	if root == "" || !filepath.IsAbs(root) {
		return "", false
	}
	return filepath.Clean(root), true
}
