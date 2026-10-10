"""Exercise first-run initialization without accessing real credentials or storage."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name("init-local-env.sh").resolve()
KEYS = ("TANK_API_TOKEN", "TANK_NODE_TOKEN", "TANK_SESSION_SECRET")


def start(directory, extra_env=None):
    env = {key: value for key, value in os.environ.items() if not key.startswith("TANK_")}
    env.update(extra_env or {})
    return subprocess.Popen(
        ["bash", "-c", 'source "$1"', "init", str(SCRIPT)],
        cwd=directory, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )


def finish(process):
    stdout, stderr = process.communicate(timeout=15)
    if process.returncode != 0:
        raise AssertionError("Local environment initialization failed.")
    if stdout or stderr:
        raise AssertionError("Environment initialization must not print key material.")


def settings(path):
    return dict(line.split("=", 1) for line in path.read_text().splitlines() if "=" in line)


class LocalEnvironmentTests(unittest.TestCase):
    def test_first_start_and_repeat_preserve_keys(self):
        with tempfile.TemporaryDirectory(prefix="tank-env-test-") as directory:
            path = Path(directory) / ".env.tank-local"
            finish(start(directory))
            before = path.read_bytes()
            for key in KEYS:
                value = settings(path).get(key, "")
                self.assertTrue(len(value) == 64 and all(c in "0123456789abcdef" for c in value), "Expected a generated secret.")
            self.assertTrue(path.stat().st_mode & 0o777 == 0o600, "Secrets must use private file permissions.")
            finish(start(directory))
            self.assertTrue(path.read_bytes() == before, "Repeated startup must preserve secrets.")

    def test_upgrade_preserves_existing_settings_without_final_newline(self):
        with tempfile.TemporaryDirectory(prefix="tank-env-test-") as directory:
            path = Path(directory) / ".env.tank-local"
            original = "TANK_NODE_TOKEN=" + "a" * 64 + "\nTANK_API_TOKEN=" + "b" * 64 + "\nTANK_CHAIN_ID=31337"
            path.write_text(original)
            finish(start(directory))
            self.assertTrue(path.read_text().startswith(original + "\n"), "Existing settings must be retained.")
            self.assertTrue(len(settings(path).get("TANK_SESSION_SECRET", "")) == 64, "Upgrade must add a session secret.")
            self.assertTrue(settings(path).get("TANK_CHAIN_ID") == "31337")

    def test_parallel_start_generates_only_one_set_of_keys(self):
        with tempfile.TemporaryDirectory(prefix="tank-env-test-") as directory:
            processes = [start(directory) for _ in range(4)]
            for process in processes:
                finish(process)
            lines = (Path(directory) / ".env.tank-local").read_text().splitlines()
            for key in KEYS:
                self.assertTrue(sum(line.startswith(key + "=") for line in lines) == 1, "Concurrent launchers must share generated secrets.")

    def test_codespaces_origin_is_persisted_and_explicit_setting_preserved(self):
        with tempfile.TemporaryDirectory(prefix="tank-env-test-") as directory:
            path = Path(directory) / ".env.tank-local"
            finish(start(directory, {"CODESPACE_NAME": "tank-origin-test", "PORT": "3443",
                                     "GITHUB_CODESPACES_PORT_FORWARDING_DOMAIN": "app.github.dev"}))
            self.assertEqual(settings(path).get("TANK_FRONTEND_ORIGIN"),
                             "https://tank-origin-test-3443.app.github.dev")
            before = path.read_bytes()
            finish(start(directory, {"CODESPACE_NAME": "different-space", "PORT": "3000"}))
            self.assertTrue(path.read_bytes() == before, "An existing pinned origin must be preserved.")

    def test_symlink_is_rejected_without_modifying_target(self):
        with tempfile.TemporaryDirectory(prefix="tank-env-test-") as directory:
            target = Path(directory) / "target"
            target.write_text("private settings")
            (Path(directory) / ".env.tank-local").symlink_to(target)
            process = start(directory)
            process.communicate(timeout=15)
            self.assertTrue(process.returncode != 0, "A symlink must be rejected.")
            self.assertTrue(target.read_text() == "private settings")


if __name__ == "__main__":
    unittest.main()
