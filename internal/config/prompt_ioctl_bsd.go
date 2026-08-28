//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package config

import "golang.org/x/sys/unix"

const (
	ioctlReadTermios  = unix.TIOCGETA
	ioctlWriteTermios = unix.TIOCSETA
)
