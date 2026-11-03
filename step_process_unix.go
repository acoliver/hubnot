//go:build unix

package main

import (
	"os/exec"
	"syscall"
	"time"
)

// stepWaitDelay bounds how long a finished or cancelled step waits for its
// output pipes to close.
const stepWaitDelay = 10 * time.Second

// isolateStepProcesses runs a host step in its own process group. A step's
// descendants (a display server, a build or test server, a daemonized
// helper) inherit its output pipes and open files; killing only the direct
// child on timeout left them running, holding the pipes, so the runner's
// wait never returned and no result was recorded. Cancellation now kills
// the whole group, and Wait gives up on the pipes after stepWaitDelay.
func isolateStepProcesses(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = stepWaitDelay
}

// reapStepProcesses kills whatever is left of a step's process group after
// the step returned, so nothing it started outlives it.
func reapStepProcesses(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
