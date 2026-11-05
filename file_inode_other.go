//go:build !unix

package main

import "os"

// Size and modification time alone identify a journal file here.
func fileInode(os.FileInfo) uint64 { return 0 }
