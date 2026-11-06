//go:build !unix

package main

// restrictServeUmask has nothing to restrict where there is no umask.
func restrictServeUmask() {}
