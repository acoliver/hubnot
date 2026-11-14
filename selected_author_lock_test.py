"""Worker locks must outlive a killed Python worker while its runner and gate live.

A stand-in `hn runner once` is a small Go program, like the real native runner:
it starts a gate in its own process group and waits. The driver takes the
worker lock, then runs the stand-in through the patched Session.run.
"""

import fcntl
import os
import signal
import subprocess
import sys
import tempfile
import textwrap
import time
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent

FAKE_HN = textwrap.dedent('''
    package main

    import (
    	"os"
    	"os/exec"
    	"strconv"
    	"syscall"
    )

    func main() {
    	gate := exec.Command("sleep", "300")
    	gate.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
    	if err := gate.Start(); err != nil {
    		os.Exit(2)
    	}
    	_ = os.WriteFile(os.Getenv("FAKE_GATE_PID"), []byte(strconv.Itoa(gate.Process.Pid)), 0o600)
    	_ = gate.Wait()
    }
''')

DRIVER = textwrap.dedent('''
    import fcntl, sys
    sys.path.insert(0, sys.argv[1])
    import selected_author
    session = object.__new__(selected_author.Session)
    session.held_locks = ()
    with open(sys.argv[2], "a+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if sys.argv[4] == "pass":
            session.held_locks = (lock.fileno(),)
        session.run(sys.argv[3], "runner", "once")
''')


def lock_is_free(path):
    with open(path, "a+") as probe:
        try:
            fcntl.flock(probe, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return False
        fcntl.flock(probe, fcntl.LOCK_UN)
        return True


def wait_for(predicate, seconds):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        if predicate():
            return True
        time.sleep(0.05)
    return predicate()


class LockInheritanceTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory()
        root = Path(cls.temporary.name)
        (root / "main.go").write_text(FAKE_HN)
        (root / "go.mod").write_text("module fakehn\n\ngo 1.21\n")
        cls.fake_hn = str(root / "fake-hn")
        subprocess.run(["go", "build", "-o", cls.fake_hn, "."], cwd=root, check=True, timeout=120,
                       env={**os.environ, "CGO_ENABLED": "0", "GOFLAGS": ""})

    @classmethod
    def tearDownClass(cls):
        cls.temporary.cleanup()

    def run_case(self, mode):
        root = Path(tempfile.mkdtemp(dir=self.temporary.name))
        lock = root / "runners.lock"
        gate_pid_file = root / "gate.pid"
        driver = subprocess.Popen([sys.executable, "-B", "-c", DRIVER, str(HERE), str(lock), self.fake_hn, mode],
                                  env={**os.environ, "FAKE_GATE_PID": str(gate_pid_file)})
        gate = None
        try:
            self.assertTrue(wait_for(gate_pid_file.exists, 30), "stand-in runner never started its gate")
            gate = int(gate_pid_file.read_text())
            self.assertFalse(lock_is_free(lock), "driver should hold the lock while running")
            driver.kill()
            driver.wait(timeout=10)
            self.assertTrue(self.alive(gate), "gate should survive the killed worker (this is the risk)")
            return lock, gate
        except BaseException:
            if gate:
                self.kill_group(gate)
            raise

    @staticmethod
    def alive(pid):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return False
        return True

    @staticmethod
    def kill_group(pid):
        try:
            os.killpg(pid, signal.SIGKILL)
        except ProcessLookupError:
            pass

    def test_lock_stays_held_until_runner_and_gate_exit(self):
        lock, gate = self.run_case("pass")
        try:
            time.sleep(0.5)
            self.assertFalse(lock_is_free(lock), "lock released while the gate is still running")
        finally:
            self.kill_group(gate)
        self.assertTrue(wait_for(lambda: lock_is_free(lock), 15), "lock not released after gate and runner exited")

    def test_without_passing_locks_the_lock_drops_with_the_worker(self):
        lock, gate = self.run_case("none")
        try:
            self.assertTrue(wait_for(lambda: lock_is_free(lock), 5),
                            "control: old behaviour should free the lock while the gate runs")
        finally:
            self.kill_group(gate)


if __name__ == "__main__":
    unittest.main(verbosity=2)
