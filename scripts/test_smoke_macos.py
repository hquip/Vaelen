"""Exercise the watchdog against real processes, including a stuck shutdown."""

import importlib.util
from pathlib import Path
import signal
import subprocess
import sys
import unittest

spec = importlib.util.spec_from_file_location(
    "smoke_macos", Path(__file__).with_name("smoke-macos.py"))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class ProcessWatchdogTest(unittest.TestCase):
    def start_child(self, code):
        child = subprocess.Popen([sys.executable, "-u", "-c", code],
                                 stdout=subprocess.PIPE, text=True)
        self.addCleanup(smoke.stop_process, child)
        self.addCleanup(child.stdout.close)
        self.assertEqual(child.stdout.readline().strip(), "ready")
        return child

    def test_running_child_is_stopped(self):
        child = self.start_child("import time; print('ready'); time.sleep(60)")
        smoke.check_running(child, seconds=0.1)
        smoke.stop_process(child)
        self.assertIsNotNone(child.poll())

    def test_early_clean_exit_is_a_startup_failure(self):
        child = self.start_child("print('ready')")
        with self.assertRaisesRegex(RuntimeError, "exit code 0"):
            smoke.check_running(child, seconds=5)

    @unittest.skipIf(sys.platform == "win32", "SIGTERM handlers need POSIX")
    def test_ignoring_sigterm_is_force_killed(self):
        child = self.start_child(
            "import signal, time; signal.signal(signal.SIGTERM, signal.SIG_IGN); "
            "print('ready'); time.sleep(60)")
        smoke.stop_process(child, timeout=0.2)
        self.assertEqual(child.returncode, -signal.SIGKILL)


if __name__ == "__main__":
    unittest.main()
