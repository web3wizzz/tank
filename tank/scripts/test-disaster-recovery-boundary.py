#!/usr/bin/env python3
"""Loss-of-authority regression tests; no remote custody or chain evidence.

Use disposable databases and injected process controls. A successful validator must
never substitute for the missing newest live comparison baseline.
"""

import importlib.util
from pathlib import Path
import sqlite3
import tempfile
import hashlib
import signal
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location(
    "restore_fixtures", Path(__file__).with_name("test-pilot-restore.py")
)
fixtures = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixtures)
restore = fixtures.restore


class LostAuthorityTests(unittest.TestCase):
    def setUp(self):
        # Process ownership/signalling is injected; existing Linux PID-descriptor
        # tests cover those boundaries. No actual services are touched here.
        temp = tempfile.TemporaryDirectory(prefix="tank-lost-authority-")
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        root.chmod(0o700)
        live = root / "live.sqlite"
        with fixtures.connect(live) as db:
            db.executescript("""PRAGMA user_version=8;
            CREATE TABLE api_credentials(id TEXT PRIMARY KEY,revoked INTEGER);
            INSERT INTO api_credentials VALUES('synthetic',1);
            CREATE TABLE registration_transactions(account TEXT,nonce TEXT,raw_tx BLOB);
            INSERT INTO registration_transactions VALUES('synthetic','1',X'01AA');
            CREATE TABLE registration_spend_accounts(account TEXT,budget TEXT,reserved TEXT);
            INSERT INTO registration_spend_accounts VALUES('synthetic','100000000000000000','5480020000000000');
            """)
        live.chmod(0o600)
        deployment = root / "deployment-journal"
        deployment.write_bytes(b"synthetic immutable deployment")
        deployment.chmod(0o600)
        validator = root / "tank-restore-check"
        validator.write_bytes(b"#!/bin/sh\nexit 0\n")
        validator.chmod(0o700)
        services = [{"role": role, "pid": i + 1000} for i, role in enumerate(
            ("coordinator", "registration-timer", "node"))]
        config = {
            "version": 1, "fixture_directory": str(root),
            "live_database": str(live), "snapshot_database": str(root / "snapshot.sqlite"),
            "candidate_database": str(root / "candidate.sqlite"),
            "lane_lock": str(root / "lane.lock"), "journal": str(root / "controller.json"),
            "deployment_journals": [str(deployment)], "validator": str(validator),
            "validator_sha256": hashlib.sha256(validator.read_bytes()).hexdigest(),
            "services": services, "failure_targets": [services[2]["pid"]],
            "timeout_seconds": 5,
        }
        states = {service["pid"]: "S" for service in services}

        def injected_signal(service, sig):
            states[service["pid"]] = "T" if sig == signal.SIGSTOP else "S"

        self.enterContext(mock.patch.object(restore, "identity", side_effect=lambda s: states[s["pid"]]))
        self.enterContext(mock.patch.object(restore, "signal_service", side_effect=injected_signal))
        # Reuse the fixture's execution and gate helpers, without its processes.
        self.fixture = fixtures.RestoreTests()
        self.fixture.live = live
        self.fixture.config = config

    def assert_stopped_authority(self):
        self.assertTrue(self.fixture.gate().exists())
        for service in self.fixture.config["services"]:
            if service["role"] in ("coordinator", "registration-timer"):
                self.assertIn(restore.identity(service), ("T", "t"))

    def test_old_snapshot_does_not_replace_lost_newest_state(self):
        f = self.fixture
        snapshot = Path(f.config["snapshot_database"])
        with fixtures.connect(f.live) as source, fixtures.connect(snapshot) as saved:
            source.backup(saved)
        snapshot.chmod(0o600)
        original_snapshot = restore.digest(snapshot)
        with fixtures.connect(f.live) as newest:
            newest.execute("UPDATE api_credentials SET revoked=2")
            newest.execute("UPDATE registration_transactions SET nonce='2',raw_tx=X'02DDFF'")
            newest.execute("UPDATE registration_spend_accounts SET reserved='10960040000000000'")
        # Destroy only synthetic fixture state, modelling loss of the source disk.
        f.live.unlink()
        with mock.patch.object(restore.subprocess, "run") as validator:
            with self.assertRaises((restore.Blocked, sqlite3.OperationalError)):
                f.execute()
            validator.assert_not_called()
        self.assertFalse(f.live.exists())  # No empty authority database recreated.
        self.assertFalse(Path(f.config["candidate_database"]).exists())
        self.assertEqual(restore.digest(snapshot), original_snapshot)
        self.assert_stopped_authority()

    def test_interrupted_copy_cannot_resume_without_live_authority(self):
        f = self.fixture

        def interrupt(phase):
            if phase == "copied":
                raise KeyboardInterrupt

        with self.assertRaises(KeyboardInterrupt):
            f.execute(checkpoint=interrupt)
        candidate = Path(f.config["candidate_database"])
        candidate_digest = restore.digest(candidate)
        candidate_inode = candidate.stat().st_ino
        f.live.unlink()  # Only the disposable synthetic database.
        with mock.patch.object(restore.subprocess, "run") as validator:
            with self.assertRaises((restore.Blocked, sqlite3.OperationalError)):
                f.execute(resume=True)
            validator.assert_not_called()
        self.assertFalse(f.live.exists())
        self.assertEqual(candidate.stat().st_ino, candidate_inode)
        self.assertEqual(restore.digest(candidate), candidate_digest)
        self.assertTrue(Path(str(candidate) + ".restore-gate").exists())
        self.assert_stopped_authority()


if __name__ == "__main__":
    unittest.main()
