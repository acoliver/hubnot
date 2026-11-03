//go:build !unix

package main

import (
	"os/exec"
	"time"
)

const stepWaitDelay = 10 * time.Second

// isolateStepProcesses only bounds the wait for output pipes where process
// groups are unavailable.
func isolateStepProcesses(command *exec.Cmd) {
	command.WaitDelay = stepWaitDelay
}

func reapStepProcesses(*exec.Cmd) {}
