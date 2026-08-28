//go:build !windows && !linux && !darwin

package config

import (
	"fmt"
	"os"
	"runtime"
)

func openRestrictedPasswordFile(string) (*os.File, error) {
	return nil, fmt.Errorf("secure credential files are unsupported on %s", runtime.GOOS)
}
