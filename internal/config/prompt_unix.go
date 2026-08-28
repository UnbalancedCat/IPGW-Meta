//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package config

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func terminalIsInteractive(input *os.File) bool {
	_, err := unix.IoctlGetTermios(int(input.Fd()), ioctlReadTermios)
	return err == nil
}

func readTerminalPassword(input *os.File) ([]byte, error) {
	fd := int(input.Fd())
	state, err := unix.IoctlGetTermios(fd, ioctlReadTermios)
	if err != nil {
		return nil, err
	}
	hidden := *state
	hidden.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, ioctlWriteTermios, &hidden); err != nil {
		return nil, err
	}
	defer unix.IoctlSetTermios(fd, ioctlWriteTermios, state)
	line, err := bufio.NewReader(input).ReadString('\n')
	return []byte(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), err
}
