#!/usr/bin/env python3
"""Isolated orchestration fixtures with owned real local sleeping services.

Chain proof behavior is injected here; cryptographic/RPC checks are covered by
the Go restore validator tests. These tests provide no hosted evidence.
"""

import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
import fcntl
from unittest import mock

spec = importlib.util.spec_from_file_location(
    "restore", Path(__file__).with_name("pilot-restore.py")
)
restore = importlib.util.module_from_spec(spec)
spec.loader.exec_module(restore)


@contextlib.contextmanager
def connect(path, **kwargs):
    with contextlib.closing(sqlite3.connect(path, **kwargs)) as db:
        with db:
            yield db


class RestoreTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="tank-restore-fixture-")
        self.root = Path(self.temp.name)
        self.root.chmod(0o700)
        self.children = []
        self.addCleanup(self.cleanup)
        self.write(".tank-drill-fixture", b"isolated local fixture")
        self.deployment = self.write(
            "deployment-journal", b"synthetic immutable deployment bytes"
        )
        self.live = self.root / "live.sqlite"
        with connect(self.live) as db:
            db.executescript("""PRAGMA user_version=8;
            CREATE TABLE api_credentials(id TEXT PRIMARY KEY,revoked INTEGER);
            INSERT INTO api_credentials VALUES('synthetic-user',1);
            CREATE TABLE registration_transactions(account TEXT PRIMARY KEY,nonce TEXT,raw_tx BLOB);
            INSERT INTO registration_transactions VALUES('synthetic-account','1',X'01AABBCC');
            CREATE TABLE registration_spend_accounts(account TEXT PRIMARY KEY,budget TEXT,reserved TEXT);
            INSERT INTO registration_spend_accounts VALUES('synthetic-account','100000000000000000','5480020000000000');
            CREATE TABLE registration_jobs(file_id TEXT PRIMARY KEY,status TEXT,tx_hash TEXT);
            INSERT INTO registration_jobs VALUES('synthetic-file','registered','synthetic-transaction');
            """)
        self.live.chmod(0o600)
        validator = self.write("tank-restore-check", b"#!/bin/sh\nexit 0\n")
        validator.chmod(0o700)
        services = []
        for role in ("coordinator", "registration-timer", "node"):
            child = subprocess.Popen(
                [sys.executable, "-c", "import time; time.sleep(120)"],
                cwd=self.root,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            self.children.append(child)
            expected = hashlib.sha256(
                (sys.executable + "\0-c\0import time; time.sleep(120)\0").encode()
            ).hexdigest()
            deadline = time.monotonic() + 2
            while restore.digest(Path(f"/proc/{child.pid}/cmdline")) != expected:
                if time.monotonic() > deadline:
                    raise AssertionError("Synthetic service did not exec")
                time.sleep(0.005)
            stats = (
                Path(f"/proc/{child.pid}/stat").read_text().rsplit(")", 1)[1].split()
            )
            services.append(
                {
                    "role": role,
                    "pid": child.pid,
                    "start_ticks": stats[19],
                    "command_sha256": restore.digest(
                        Path(f"/proc/{child.pid}/cmdline")
                    ),
                }
            )
        self.config = {
            "version": 1,
            "fixture_directory": str(self.root),
            "live_database": str(self.live),
            "snapshot_database": str(self.root / "snapshot.sqlite"),
            "candidate_database": str(self.root / "candidate.sqlite"),
            "lane_lock": str(self.root / "lane.lock"),
            "journal": str(self.root / "controller.json"),
            "deployment_journals": [str(self.deployment)],
            "validator": str(validator),
            "validator_sha256": restore.digest(validator),
            "services": services,
            "failure_targets": [services[2]["pid"]],
            "timeout_seconds": 5,
        }
        self.config_path = self.write("config.json", json.dumps(self.config).encode())

    def cleanup(self):
        for child in self.children:
            if child.poll() is None:
                os.kill(child.pid, signal.SIGCONT)
                child.terminate()
                try:
                    child.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait()
        self.temp.cleanup()  # Only our generated fixture data.

    def write(self, name, data):
        path = self.root / name
        path.write_bytes(data)
        path.chmod(0o600)
        return path

    def execute(self, **kwargs):
        with contextlib.redirect_stdout(io.StringIO()):
            restore.run(self.config, **kwargs)

    def gate(self):
        return Path(self.config["lane_lock"] + ".restore-gate")

    def test_private_schema_and_exact_service_identity(self):
        self.assertEqual(restore.load(self.config_path), self.config)
        self.config["services"][0]["start_ticks"] = "0"
        self.config_path.write_text(json.dumps(self.config))
        with self.assertRaises(restore.Blocked):
            restore.load(self.config_path)

    def test_dry_run_changes_nothing(self):
        before = set(self.root.iterdir())
        self.execute(dry_run=True)
        self.assertEqual(set(self.root.iterdir()), before)
        self.assertTrue(
            all(restore.identity(s) not in ("T", "t") for s in self.config["services"])
        )

    def test_concurrent_worker_refused_before_mutation(self):
        with open(self.config["lane_lock"], "w") as lane:
            os.chmod(lane.name, 0o600)
            fcntl.flock(lane, fcntl.LOCK_EX | fcntl.LOCK_NB)
            with self.assertRaises(restore.Blocked):
                self.execute()
        self.assertFalse(self.gate().exists())
        self.assertFalse(Path(self.config["journal"]).exists())

    def test_success_preserves_signed_bytes_revocation_spend_and_deployment(self):
        original = restore.digest(self.live)
        deployment = restore.digest(self.deployment)
        self.execute()
        self.assertEqual(restore.digest(self.live), original)
        self.assertEqual(restore.digest(self.deployment), deployment)
        self.assertFalse(self.gate().exists())
        self.assertTrue(
            Path(self.config["candidate_database"] + ".restore-gate").exists()
        )
        with connect(self.config["candidate_database"]) as db:
            self.assertEqual(
                db.execute("SELECT raw_tx FROM registration_transactions").fetchone()[
                    0
                ],
                bytes.fromhex("01aabbcc"),
            )
            self.assertEqual(
                db.execute("SELECT revoked FROM api_credentials").fetchone()[0], 1
            )
            self.assertEqual(
                db.execute(
                    "SELECT reserved FROM registration_spend_accounts"
                ).fetchone()[0],
                "5480020000000000",
            )
            self.assertEqual(
                db.execute("SELECT count(*) FROM registration_jobs").fetchone()[0], 1
            )

    def test_stale_backup_cannot_undo_revocation_or_spending(self):
        with connect(self.live) as source, connect(
            self.config["snapshot_database"]
        ) as target:
            source.backup(target)
        Path(self.config["snapshot_database"]).chmod(0o600)
        with connect(self.live) as db:
            db.execute("UPDATE api_credentials SET revoked=2")
        with self.assertRaises(restore.Blocked):
            self.execute()
        self.assertTrue(self.gate().exists())
        self.assertFalse(Path(self.config["candidate_database"]).exists())

    def test_interrupted_restore_recovers_without_replacing_candidate(self):
        def interrupt(phase):
            if phase == "copied":
                raise KeyboardInterrupt

        with self.assertRaises(KeyboardInterrupt):
            self.execute(checkpoint=interrupt)
        candidate = Path(self.config["candidate_database"])
        original = restore.digest(candidate)
        inode = candidate.stat().st_ino
        self.assertTrue(self.gate().exists())
        timer = self.config["services"][1]
        self.assertIn(restore.identity(timer), ("T", "t"))
        self.execute(resume=True)
        self.assertEqual(candidate.stat().st_ino, inode)
        self.assertEqual(restore.digest(candidate), original)
        self.assertFalse(self.gate().exists())
        self.assertNotIn(restore.identity(timer), ("T", "t"))

    def test_ambiguous_reconciliation_blocks_until_verified(self):
        validator = Path(self.config["validator"])
        validator.write_text("#!/bin/sh\nexit 1\n")
        self.config["validator_sha256"] = restore.digest(validator)
        with self.assertRaises(restore.Blocked):
            self.execute()
        self.assertTrue(self.gate().exists())
        # Same pinned validator can change observations without changing its binary.
        # Fixture injects exit outcome through environment only, no chain evidence.

    def test_metadata_writers_are_fenced(self):
        def check(phase):
            if phase == "copied":
                with connect(self.live, timeout=0) as db:
                    with self.assertRaises(sqlite3.OperationalError):
                        db.execute("UPDATE api_credentials SET revoked=0")

        self.execute(checkpoint=check)

    def test_deployment_journal_change_keeps_gate_closed(self):
        def change(phase):
            if phase == "copied":
                self.deployment.write_bytes(b"externally changed synthetic fixture")

        with self.assertRaises(restore.Blocked):
            self.execute(checkpoint=change)
        self.assertTrue(self.gate().exists())

    def test_bounded_validator_timeout_keeps_gate_closed(self):
        validator = Path(self.config["validator"])
        validator.write_text("#!/usr/bin/env python3\nimport time\ntime.sleep(10)\n")
        self.config["validator_sha256"] = restore.digest(validator)
        self.config["timeout_seconds"] = 1
        with self.assertRaises(subprocess.TimeoutExpired):
            self.execute()
        self.assertTrue(self.gate().exists())

    def test_existing_candidate_never_overwritten(self):
        candidate = self.write("candidate.sqlite", b"owned pre-existing data")
        with self.assertRaises(restore.Blocked):
            self.execute()
        self.assertEqual(candidate.read_bytes(), b"owned pre-existing data")

    def test_interruption_between_pause_intent_and_signal_can_resume(self):
        count = 0

        def interrupt(phase):
            nonlocal count
            if phase == "pause-intent":
                count += 1
                if count == 2:
                    raise KeyboardInterrupt

        with self.assertRaises(KeyboardInterrupt):
            self.execute(checkpoint=interrupt)
        self.assertTrue(self.gate().exists())
        self.execute(resume=True)
        self.assertFalse(self.gate().exists())

    def test_pid_identity_failure_cannot_signal_replacement(self):
        with mock.patch.object(
            restore, "identity", side_effect=restore.Blocked("fixture")
        ), mock.patch.object(signal, "pidfd_send_signal") as send:
            with self.assertRaises(restore.Blocked):
                restore.signal_service(self.config["services"][0], signal.SIGSTOP)
            send.assert_not_called()

    def test_table_boundaries_are_part_of_snapshot_digest(self):
        with connect(self.live) as db:
            db.executescript(
                "CREATE TABLE a(value TEXT); CREATE TABLE b(value TEXT); INSERT INTO a VALUES('fixture');"
            )
            first = restore.logical_digest(db)
            db.executescript("DELETE FROM a; INSERT INTO b VALUES('fixture');")
            self.assertNotEqual(restore.logical_digest(db), first)

    def test_cleanup_journal_failure_releases_lock_and_preserves_gate(self):
        durable = restore.durable
        writers = [self.config["services"][0]["pid"], self.config["services"][1]["pid"]]

        def fail(path, state, **kwargs):
            if (
                path == Path(self.config["journal"])
                and state.get("phase") == "copied"
                and state.get("paused") == writers
            ):
                raise OSError("synthetic journal unavailable")
            return durable(path, state, **kwargs)

        def interrupt(phase):
            if phase == "copied":
                raise KeyboardInterrupt

        with mock.patch.object(restore, "durable", side_effect=fail):
            with self.assertRaises(OSError):
                self.execute(checkpoint=interrupt)
        self.assertTrue(self.gate().exists())
        with open(self.config["lane_lock"], "r+") as lane:
            fcntl.flock(lane, fcntl.LOCK_EX | fcntl.LOCK_NB)

    def test_failed_restore_keeps_metadata_writers_quiescent(self):
        def interrupt(phase):
            if phase == "copied":
                raise KeyboardInterrupt

        with self.assertRaises(KeyboardInterrupt):
            self.execute(checkpoint=interrupt)
        for service in self.config["services"][:2]:
            self.assertIn(restore.identity(service), ("T", "t"))
        self.assertNotIn(restore.identity(self.config["services"][2]), ("T", "t"))
        self.execute(resume=True)
        for service in self.config["services"]:
            self.assertNotIn(restore.identity(service), ("T", "t"))

    def test_killed_controller_recovers_its_durable_phase(self):
        marker = self.root / "validator-started"
        validator = Path(self.config["validator"])
        validator.write_text(
            "#!/usr/bin/env python3\nimport pathlib,time\n"
            f"pathlib.Path({str(marker)!r}).write_text('fixture')\ntime.sleep(0.5)\n"
        )
        self.config["validator_sha256"] = restore.digest(validator)
        self.config_path.write_text(json.dumps(self.config))
        command = [
            sys.executable,
            str(Path(__file__).with_name("pilot-restore.py")),
            "--config",
            str(self.config_path),
            "--execute-local-fixture",
        ]
        controller = subprocess.Popen(
            command, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
        )
        self.children.append(controller)
        deadline = time.monotonic() + 3
        while not marker.exists():
            if controller.poll() is not None or time.monotonic() > deadline:
                self.fail("Synthetic controller failed before reconciliation")
            time.sleep(0.01)
        controller.kill()
        controller.wait(timeout=2)
        self.assertTrue(self.gate().exists())
        candidate = Path(self.config["candidate_database"])
        inode = candidate.stat().st_ino
        time.sleep(
            0.6
        )  # Let the bounded read-only fixture child exit after parent death.
        result = subprocess.run(
            command + ["--resume"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=5,
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual(candidate.stat().st_ino, inode)
        self.assertFalse(self.gate().exists())


if __name__ == "__main__":
    unittest.main()
