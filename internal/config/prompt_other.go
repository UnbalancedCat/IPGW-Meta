//go:build !windows && !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly

package config

import (
	"fmt"
	"os"
)

func terminalIsInteractive(*os.File) bool { return false }

func readTerminalPassword(*os.File) ([]byte, error) {
	return nil, fmt.Errorf("secure terminal prompting is unsupported on this platform")
}
