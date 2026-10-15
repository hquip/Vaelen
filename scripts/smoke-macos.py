#!/usr/bin/env python3
"""Launch the packaged app without AppleScript automation permission prompts."""

import os
from pathlib import Path
import subprocess
import sys
import tempfile


def stop_process(process, timeout=5):
    """Reap only our child, with bounded waits even if it ignores SIGTERM."""
    if process.poll() is not None:
        return
    try:
        process.terminate()
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=timeout)


def check_running(process, seconds=10):
    try:
        code = process.wait(timeout=seconds)
    except subprocess.TimeoutExpired:
        return
    raise RuntimeError(f"VSM.app exited during startup (exit code {code})")


def main():
    if sys.platform != "darwin":
        raise RuntimeError("The macOS app smoke test must run on macOS")
    archive, screenshot = (Path(arg).resolve() for arg in sys.argv[1:])
    screenshot.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="vsm smoke ") as work:
        work = Path(work)
        subprocess.run(["ditto", "-x", "-k", str(archive), str(work)],
                       check=True, timeout=30)
        app = work / "VSM.app"
        subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)],
                       check=True, timeout=30)
        env = dict(os.environ, VSM_HOME=str(work / "vsm-home"))
        log_path = work / "gui.log"
        with log_path.open("wb") as log:
            # Match Finder's root working directory and avoid the runner's config.
            process = subprocess.Popen([str(app / "Contents/MacOS/vsm-gui")],
                                       cwd="/", env=env, stdout=log,
                                       stderr=subprocess.STDOUT)
            try:
                check_running(process)
                print(f"VSM.app running after 10 seconds (pid {process.pid})", flush=True)
                try:
                    subprocess.run(["screencapture", "-x", str(screenshot)],
                                   check=True, timeout=10)
                except (subprocess.SubprocessError, OSError) as error:
                    print(f"Screenshot unavailable: {error}", flush=True)
                if process.poll() is not None:
                    raise RuntimeError("VSM.app exited while capturing the screenshot")
            finally:
                stop_process(process)
                log.flush()
                print(log_path.read_text(errors="replace")[-8000:], flush=True)


if __name__ == "__main__":
    main()
