package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/UnbalancedCat/ipgw-meta/internal/app"
	"github.com/UnbalancedCat/ipgw-meta/internal/cli"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

var version = "dev"

func main() {
	paths, err := config.DefaultPaths()
	if err != nil {
		os.Exit(cli.StartupFailure(os.Args[1:], os.Stdout, os.Stderr, err))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	exit := cli.Execute(ctx, cli.Options{
		Mode: cli.ModeMeta, Args: os.Args[1:], Paths: paths,
		NewGateway: func(paths config.Paths) cli.Gateway { return app.New(paths, os.Stdin, os.Stderr) },
		Input:      os.Stdin, Out: os.Stdout, Err: os.Stderr, IsTTY: cli.IsSafeTerminal(os.Stdin, os.Stderr), Version: version,
	})
	os.Exit(exit)
}
