package main

import (
	"os"

	"github.com/UnbalancedCat/ipgw-meta/internal/launcher"
)

var (
	version        = "dev"
	installDefault = "meta"
)

func main() {
	os.Exit(launcher.Execute(launcher.Options{
		Args:           os.Args[1:],
		InstallDefault: installDefault,
		Stdin:          os.Stdin,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
	}))
}
