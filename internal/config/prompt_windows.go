//go:build windows

package config

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func terminalIsInteractive(input *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(input.Fd()), &mode) == nil
}

func readTerminalPassword(input *os.File) ([]byte, error) {
	handle := windows.Handle(input.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return nil, err
	}
	if err := windows.SetConsoleMode(handle, mode&^windows.ENABLE_ECHO_INPUT); err != nil {
		return nil, err
	}
	defer windows.SetConsoleMode(handle, mode)
	line, err := bufio.NewReader(input).ReadString('\n')
	return []byte(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), err
}
