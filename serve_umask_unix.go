//go:build unix

package main

import "syscall"

// restrictServeUmask makes every file and directory a serve session or its
// Git children create private (0600 files, 0700 directories). The state
// checks require exactly that under state/tmp, and a session's own temporary
// repository (for example the audit object check) is visible there to every
// session that starts while it exists.
func restrictServeUmask() {
	syscall.Umask(0o077)
}
