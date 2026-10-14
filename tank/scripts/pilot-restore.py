#!/usr/bin/env python3
"""Isolated local restore orchestration. Remote execution and activation are disabled.

Only an explicitly marked private fixture directory and exact owned process
identities are accepted. Real hosted activation remains a separate future gate.
"""

import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import tempfile
import time


class Blocked(Exception):
    pass


class RedactedParser(argparse.ArgumentParser):
    def error(self, message):
        raise Blocked("Invalid controller arguments; supplied values withheld.")


def require(value):
    if not value:
        raise Blocked("Local restore safety check failed; values withheld.")


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def private(path, exists=True):
    path = Path(path)
    require(path.is_absolute() and str(path) == str(path.resolve()))
    require(all(not part.is_symlink() for part in (path, *path.parents)))
    parent = path.parent.stat()
    require(parent.st_uid == os.geteuid() and parent.st_mode & 0o077 == 0)
    if exists or path.exists():
        info = path.stat()
        require(
            path.is_file()
            and info.st_uid == os.geteuid()
            and info.st_mode & 0o077 == 0
            and info.st_nlink == 1
        )
    return path


def durable(path, value, new=False):
    data = json.dumps(value, sort_keys=True).encode()
    if new:
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
    else:
        fd, temporary = tempfile.mkstemp(dir=path.parent, prefix=".restore-state-")
        try:
            with os.fdopen(fd, "wb") as stream:
                stream.write(data)
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, path)  # Only this run's private controller journal.
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def identity(service):
    pid = service["pid"]
    require(type(pid) is int and pid > 1 and pid != os.getpid())
    # Reject the controller's ancestors even if a fixture declares them owned.
    ancestor = os.getppid()
    while ancestor > 1:
        require(pid != ancestor)
        stat = Path(f"/proc/{ancestor}/stat").read_text().rsplit(")", 1)[1].split()
        ancestor = int(stat[1])
    info = Path(f"/proc/{pid}").stat()
    values = Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()
    require(info.st_uid == os.geteuid() and values[19] == service["start_ticks"])
    require(digest(Path(f"/proc/{pid}/cmdline")) == service["command_sha256"])
    return values[0]


def signal_service(service, action):
    # Pin the process before checking identity: PID recycling cannot redirect a
    # signal between /proc verification and delivery.
    fd = os.pidfd_open(service["pid"], 0)
    try:
        identity(service)
        signal.pidfd_send_signal(fd, action)
    finally:
        os.close(fd)


def load(path):
    config = json.loads(private(path).read_text())
    require(
        set(config)
        == {
            "version",
            "fixture_directory",
            "live_database",
            "snapshot_database",
            "candidate_database",
            "lane_lock",
            "journal",
            "deployment_journals",
            "validator",
            "validator_sha256",
            "services",
            "failure_targets",
            "timeout_seconds",
        }
    )
    require(
        type(config["version"]) is int
        and config["version"] == 1
        and type(config["timeout_seconds"]) is int
        and 1 <= config["timeout_seconds"] <= 90
    )
    root = Path(config["fixture_directory"])
    require(
        root.is_absolute()
        and root.is_dir()
        and root.stat().st_uid == os.geteuid()
        and root.stat().st_mode & 0o077 == 0
    )
    private(root / ".tank-drill-fixture")
    paths = []
    for key in (
        "live_database",
        "snapshot_database",
        "candidate_database",
        "lane_lock",
        "journal",
        "validator",
    ):
        value = private(config[key], exists=key in ("live_database", "validator"))
        require(value.is_relative_to(root))
        paths.append(value)
    require(
        len(set(paths)) == len(paths)
        and digest(Path(config["validator"])) == config["validator_sha256"]
    )
    require(
        isinstance(config["deployment_journals"], list)
        and config["deployment_journals"]
    )
    for item in config["deployment_journals"]:
        require(private(item).is_relative_to(root))
    require(
        len(set(config["deployment_journals"])) == len(config["deployment_journals"])
        and not set(config["deployment_journals"]).intersection(str(p) for p in paths)
    )
    require(isinstance(config["services"], list) and config["services"])
    pids = set()
    roles = []
    for service in config["services"]:
        require(set(service) == {"role", "pid", "start_ticks", "command_sha256"})
        require(
            service["role"] in ("coordinator", "registration-timer", "node")
            and service["pid"] not in pids
        )
        identity(service)
        require(Path(f"/proc/{service['pid']}/cwd").resolve() == root.resolve())
        pids.add(service["pid"])
        roles.append(service["role"])
    require(roles.count("coordinator") == 1 and roles.count("registration-timer") == 1)
    require(
        isinstance(config["failure_targets"], list)
        and config["failure_targets"]
        and all(type(pid) is int for pid in config["failure_targets"])
        and len(set(config["failure_targets"])) == len(config["failure_targets"])
    )
    require(
        set(config["failure_targets"]).issubset(
            {s["pid"] for s in config["services"] if s["role"] == "node"}
        )
    )
    return config


def database(path, deadline, writable=False):
    db = sqlite3.connect(
        path.as_uri() + ("?mode=rw" if writable else "?mode=ro"), uri=True, timeout=1
    )
    try:
        db.set_progress_handler(lambda: int(time.monotonic() >= deadline), 1000)
        require(db.execute("PRAGMA user_version").fetchone()[0] == 8)
        require(db.execute("PRAGMA integrity_check").fetchone()[0] == "ok")
        require(not db.execute("PRAGMA foreign_key_check").fetchall())
        return db
    except BaseException:
        db.close()
        raise


def logical_digest(db):
    # Include EVERY table and schema: credentials/revocations, signed bytes,
    # confirmed jobs, nonce lanes, reservations, and manifests cannot roll back.
    result = hashlib.sha256()
    schema = db.execute(
        "SELECT type,name,sql FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name"
    ).fetchall()
    result.update(json.dumps(schema).encode())
    for kind, name, _ in schema:
        if kind != "table":
            continue
        quoted = '"' + name.replace('"', '""') + '"'
        columns = db.execute(f"PRAGMA table_info({quoted})").fetchall()
        result.update(json.dumps({"table": name, "columns": columns}).encode() + b"\n")
        order = ",".join('"' + c[1].replace('"', '""') + '"' for c in columns)
        count = 0
        for row in db.execute(f"SELECT * FROM {quoted} ORDER BY {order}"):
            count += 1
            value = [
                {"blob": item.hex()} if isinstance(item, bytes) else item
                for item in row
            ]
            result.update(json.dumps(value, separators=(",", ":")).encode() + b"\n")
        result.update(json.dumps({"end_table": name, "rows": count}).encode() + b"\n")
    return result.hexdigest()


def snapshot(source, destination, deadline):
    require(not destination.exists())
    fd, temporary = tempfile.mkstemp(dir=destination.parent, prefix=".restore-copy-")
    os.close(fd)
    try:
        with contextlib.closing(sqlite3.connect(temporary)) as target:

            def progress(*_):
                require(time.monotonic() < deadline)

            source.backup(target, pages=128, progress=progress, sleep=0.01)
        with open(temporary, "rb") as stream:
            os.fsync(stream.fileno())
        os.link(temporary, destination)  # Never replace live data or a destination.
        fd = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    finally:
        os.unlink(temporary)  # Only this invocation's new unpublished staging file.


def run(config, resume=False, dry_run=False, checkpoint=lambda _: None):
    if dry_run:
        print(
            "DRY RUN: exact owned local services, node targets and private paths validated; no mutation or chain checks."
        )
        return
    deadline = time.monotonic() + config["timeout_seconds"]
    fingerprint = hashlib.sha256(
        json.dumps(config, sort_keys=True).encode()
    ).hexdigest()
    lane, journal = Path(config["lane_lock"]), Path(config["journal"])
    gate = Path(str(lane) + ".restore-gate")
    candidate = Path(config["candidate_database"])
    candidate_gate = Path(str(candidate) + ".restore-gate")
    fd = os.open(lane, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW, 0o600)
    live = None
    restored_lock = None
    state = None
    try:
        require(os.fstat(fd).st_nlink == 1)
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise Blocked("Concurrent signing/restore worker refused.") from None
        if resume:
            state = json.loads(private(journal).read_text())
            require(state["config"] == fingerprint)
            require(
                set(state)
                in (
                    {"version", "config", "phase", "paused", "deployment"},
                    {"version", "config", "phase", "paused", "deployment", "digest"},
                )
            )
            require(
                state["version"] == 1
                and state["phase"]
                in ("gated", "frozen", "copied", "reconciled", "complete")
            )
            require(
                isinstance(state["paused"], list)
                and len(set(state["paused"])) == len(state["paused"])
                and set(state["paused"]).issubset(
                    {service["pid"] for service in config["services"]}
                )
            )
            require(
                state["deployment"]
                == {
                    str(i): digest(Path(p))
                    for i, p in enumerate(config["deployment_journals"])
                }
            )
        else:
            require(
                not journal.exists()
                and not gate.exists()
                and not candidate.exists()
                and not candidate_gate.exists()
            )
            state = {
                "version": 1,
                "config": fingerprint,
                "phase": "gated",
                "paused": [],
                "deployment": {
                    str(i): digest(Path(p))
                    for i, p in enumerate(config["deployment_journals"])
                },
            }
            durable(journal, state, new=True)
        if gate.exists():
            require(json.loads(private(gate).read_text()) == {"config": fingerprint})
        else:
            durable(gate, {"config": fingerprint}, new=True)
        checkpoint("gated")
        for service in config["services"]:
            if (
                service["role"] == "node"
                and service["pid"] not in config["failure_targets"]
            ):
                continue
            require(time.monotonic() < deadline)
            if service["pid"] not in state["paused"]:
                require(identity(service) not in ("T", "t", "Z", "X"))
                state["paused"].append(service["pid"])
                durable(journal, state)
                checkpoint("pause-intent")
                signal_service(service, signal.SIGSTOP)
            else:
                require(identity(service) not in ("Z", "X"))
                if identity(service) not in ("T", "t"):
                    signal_service(service, signal.SIGSTOP)
            while identity(service) not in ("T", "t"):
                require(time.monotonic() < deadline)
                time.sleep(0.01)
        checkpoint("paused")
        live_path = Path(config["live_database"])
        live = database(live_path, deadline, writable=True)
        live.execute(
            "BEGIN IMMEDIATE"
        )  # Fence every metadata writer, including access administration.
        with contextlib.closing(database(live_path, deadline)) as source:
            expected = logical_digest(source)
            if "digest" in state:
                require(state["digest"] == expected)
            state["digest"] = expected
            state["phase"] = "frozen"
            durable(journal, state)
            backup = Path(config["snapshot_database"])
            if not backup.exists():
                snapshot(source, backup, deadline)
        with contextlib.closing(database(backup, deadline)) as saved:
            require(logical_digest(saved) == expected)
        if candidate_gate.exists():
            require(
                json.loads(private(candidate_gate).read_text())
                == {"config": fingerprint}
            )
        else:
            durable(candidate_gate, {"config": fingerprint}, new=True)
        if not candidate.exists():
            with contextlib.closing(database(backup, deadline)) as saved:
                snapshot(saved, candidate, deadline)
        restored_lock = database(candidate, deadline, writable=True)
        restored_lock.execute("BEGIN IMMEDIATE")
        require(logical_digest(restored_lock) == expected)
        state["phase"] = "copied"
        durable(journal, state)
        checkpoint("copied")
        remaining = deadline - time.monotonic()
        require(remaining > 0)
        validator_fd = os.open(
            private(config["validator"]), os.O_RDONLY | os.O_NOFOLLOW
        )
        try:
            with os.fdopen(os.dup(validator_fd), "rb") as binary:
                require(
                    hashlib.file_digest(binary, "sha256").hexdigest()
                    == config["validator_sha256"]
                )
            result = subprocess.run(
                [f"/proc/self/fd/{validator_fd}", "--db", str(candidate)],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=remaining,
                pass_fds=(validator_fd,),
                check=False,
            )
        finally:
            os.close(validator_fd)
        require(result.returncode == 0)
        with contextlib.closing(database(live_path, deadline)) as source:
            require(logical_digest(source) == expected)
        with contextlib.closing(database(candidate, deadline)) as restored:
            require(logical_digest(restored) == expected)
        require(
            state["deployment"]
            == {
                str(i): digest(Path(p))
                for i, p in enumerate(config["deployment_journals"])
            }
        )
        state["phase"] = "reconciled"
        durable(journal, state)
        checkpoint("reconciled")
        # Restore candidate NEVER becomes a second signing lane. Its gate stays.
        # Only the unchanged original live lane can resume after read-only proof.
        require(json.loads(private(gate).read_text()) == {"config": fingerprint})
        gate.unlink()
        parent_fd = os.open(gate.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(parent_fd)
        finally:
            os.close(parent_fd)
        state["phase"] = "complete"
        durable(journal, state)
        print(
            "PASS: isolated restore copy reconciled; original journals preserved; remote execution and candidate activation disabled."
        )
    finally:
        try:
            try:
                if live is not None:
                    live.close()
            finally:
                try:
                    if restored_lock is not None:
                        restored_lock.close()
                finally:
                    if state is not None:
                        # Keep metadata writers quiescent after failure so their
                        # maintenance writes cannot invalidate the resume baseline.
                        # Nodes may resume; coordinator/timer wait for reconciliation.
                        remaining = []
                        for service in config["services"]:
                            if service["pid"] not in state["paused"]:
                                continue
                            if (
                                service["role"] in ("coordinator", "registration-timer")
                                and gate.exists()
                            ):
                                remaining.append(service["pid"])
                                continue
                            try:
                                if identity(service) in ("T", "t"):
                                    signal_service(service, signal.SIGCONT)
                            except Exception:
                                remaining.append(service["pid"])
                        state["paused"] = remaining
                        durable(journal, state)
        finally:
            os.close(fd)


def main():
    parser = RedactedParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument("--resume", action="store_true")
    parser.add_argument("--execute-local-fixture", action="store_true")
    args = parser.parse_args()
    require(args.dry_run or args.execute_local_fixture)

    def interrupt(*_):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupt)
    config = load(args.config)

    def timeout(*_):
        raise Blocked("Local restore deadline exceeded.")

    signal.signal(signal.SIGALRM, timeout)
    if not args.dry_run:
        signal.setitimer(signal.ITIMER_REAL, config["timeout_seconds"])
    try:
        run(config, resume=args.resume, dry_run=args.dry_run)
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)


if __name__ == "__main__":
    try:
        main()
    except (Exception, KeyboardInterrupt):
        print(
            "Restore blocked or interrupted; private state preserved. Resume the same controller journal; signing gates remain unless reconciliation completed.",
            file=sys.stderr,
        )
        sys.exit(1)
