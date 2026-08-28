package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/UnbalancedCat/ipgw-meta/internal/doccheck"
)

func main() {
	root := flag.String("root", ".", "repository root")
	checkOnly := flag.Bool("check", false, "validate documentation without writing files")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "doccheck: unexpected positional arguments")
		os.Exit(1)
	}

	var violations []doccheck.Violation
	var err error
	changed := false
	if *checkOnly {
		violations, err = doccheck.Check(*root)
	} else {
		changed, violations, err = doccheck.Generate(*root)
		if err == nil && len(violations) == 0 {
			violations, err = doccheck.Check(*root)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "doccheck:", err)
		os.Exit(1)
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation.String())
		}
		os.Exit(1)
	}
	if *checkOnly {
		fmt.Println("doccheck: ok")
	} else if changed {
		fmt.Println("doccheck: generated " + doccheck.IndexPath)
	} else {
		fmt.Println("doccheck: index already up to date")
	}
}
