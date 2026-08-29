package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/UnbalancedCat/ipgw-meta/internal/cli"
	"github.com/UnbalancedCat/ipgw-meta/internal/livegate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(runMain(
		ctx,
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		cli.IsSafeTerminal(os.Stdin, os.Stderr),
	))
}

func runMain(
	ctx context.Context,
	args []string,
	stdin *os.File,
	stdout *os.File,
	stderr *os.File,
	isTTY bool,
) int {
	return int(livegate.Execute(ctx, livegate.Options{
		Args:   args,
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
		IsTTY:  isTTY,
	}))
}
