//go:build !unix

package main

import (
	"errors"
	"os"
)

// hn serve runs only on Unix hosts. This stub keeps the client commands
// building on every other platform.
func tryServeFileLock(*os.File) (bool, error) {
	return false, errors.New("hn serve repository locks require a Unix host with flock(2)")
}
