//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A worker that stops `hn runner once` must not leave the gate running: the
// gate's process group, including a child the step started, ends with hn.
func TestRunnerOnceStopSignalEndsTheRunningGate(t *testing.T) {
	project := newRunnerActorsProjectWithPipeline(t, writeSlowOperationalPipeline)
	command := exec.Command(project.binary, "runner", "once", "--accept-pipeline", "test",
		"--accept-actor", project.alpha.actor, "--backend", "host", "--allow-unsafe-host-execution")
	command.Dir = project.runner
	command.Env = operationalTestEnvironmentWith(t.TempDir(), nil)
	command.Stdin = strings.NewReader("")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stdout = writer
	command.Stderr = writer
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	t.Cleanup(func() { _ = command.Process.Kill() })

	var mu sync.Mutex
	var output bytes.Buffer
	accepted := make(chan struct{})
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		scanner := bufio.NewScanner(reader)
		signalled := false
		for scanner.Scan() {
			mu.Lock()
			output.WriteString(scanner.Text() + "\n")
			mu.Unlock()
			if !signalled && strings.HasPrefix(scanner.Text(), "Runner accepted request") {
				signalled = true
				close(accepted)
			}
		}
	}()
	select {
	case <-accepted:
	case <-time.After(60 * time.Second):
		t.Fatal("runner never accepted the request")
	}

	var gate []int
	deadline := time.Now().Add(20 * time.Second)
	for len(gate) < 2 && time.Now().Before(deadline) {
		gate = descendantPIDs(t, command.Process.Pid)
		time.Sleep(50 * time.Millisecond)
	}
	if len(gate) < 2 {
		t.Fatalf("expected the gate shell and its sleep child under hn, found %v", gate)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err = <-waited:
	case <-time.After(30 * time.Second):
		t.Fatal("hn runner once did not exit after SIGTERM")
	}
	<-readDone
	mu.Lock()
	text := output.String()
	mu.Unlock()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
		t.Fatalf("runner once after SIGTERM: %v\n%s", err, text)
	}
	if !strings.Contains(text, "runner stopped by signal; no result was recorded") {
		t.Fatalf("runner once did not report the stop:\n%s", text)
	}
	if strings.Contains(text, "Runner recorded") {
		t.Fatalf("a stopped runner recorded a result:\n%s", text)
	}
	for _, pid := range gate {
		alive := true
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
			if !processAlive(pid) {
				alive = false
				break
			}
		}
		if alive {
			t.Fatalf("gate process %d outlived hn runner once", pid)
		}
	}
}

func writeSlowOperationalPipeline(t *testing.T, repository string) {
	t.Helper()
	pipeline := map[string]any{
		"version": "hn.pipeline/0",
		"steps": []any{map[string]any{
			"name": "slow-gate", "command": "sh",
			"args": []string{"-c", "sleep 300; printf 'slow-gate-finished\\n'"}, "timeoutSeconds": 600,
		}},
	}
	encoded, err := json.MarshalIndent(pipeline, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(repository, ".hn", "pipelines")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "test.json"), append(encoded, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// descendantPIDs lists every process below root, from ps so it works on
// Linux and macOS alike.
func descendantPIDs(t *testing.T, root int) []int {
	t.Helper()
	listing, err := exec.Command("ps", "-A", "-o", "pid=", "-o", "ppid=").Output()
	if err != nil {
		t.Fatal(err)
	}
	children := map[int][]int{}
	for _, line := range strings.Split(string(listing), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 == nil && err2 == nil {
			children[ppid] = append(children[ppid], pid)
		}
	}
	var found []int
	queue := append([]int(nil), children[root]...)
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		found = append(found, pid)
		queue = append(queue, children[pid]...)
	}
	return found
}

func processAlive(pid int) bool {
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return false
	}
	// A zombie still answers kill 0; ps reports its state as Z.
	state, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	return !strings.HasPrefix(strings.TrimSpace(string(state)), "Z")
}
