//go:build unix

package main

import (
	"os"
	"syscall"
)

func fileInode(info os.FileInfo) uint64 {
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(sys.Ino)
	}
	return 0
}
