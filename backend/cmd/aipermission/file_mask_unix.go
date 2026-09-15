//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "syscall"

func applyPrivateFileCreationMask() {
	syscall.Umask(0o077)
}
