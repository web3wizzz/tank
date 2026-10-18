"""Bounded local fixture acceptance; credentials and ciphertext never logged."""
import hashlib
import http.cookiejar
import json
from pathlib import Path
import sys
import time
import urllib.request
import urllib.error

ROOT = Path(sys.argv[1])
MODE = sys.argv[2]
assert MODE in ("queued", "snapshot", "verify"), "Expected queued, snapshot or verify"
BASE = "http://localhost:3330"
DEADLINE = time.monotonic() + 120


def browser(filename):
    client = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    token = json.loads((ROOT / filename).read_text())["token"]
    req = urllib.request.Request(BASE + "/api/auth/session", method="POST",
        data=json.dumps({"token": token}).encode(), headers={
            "Origin": BASE, "X-Tank-Workspace": "1", "Content-Type": "application/json"})
    with client.open(req, timeout=15) as response:
        assert response.status == 200, "Fixture login failed"
    return client


def get(client, path):
    req = urllib.request.Request(BASE + path, headers={"Origin": BASE, "X-Tank-Workspace": "1"})
    try:
        with client.open(req, timeout=15) as response:
            data = response.read(16 * 1024 * 1024 + 1)
            assert len(data) <= 16 * 1024 * 1024, "Oversized fixture response"
            return response.status, data
    except urllib.error.HTTPError as error:
        return error.code, b""


alice, bob = browser("user-a.json"), browser("user-b.json")
status, data = get(alice, "/api/tank/files")
assert status == 200
ids = json.loads(data)["file_ids"]
assert ids, "Browser fixture uploaded no file"
records = []
for file_id in ids:
    assert time.monotonic() < DEADLINE, "Fixture acceptance deadline exceeded"
    status, ciphertext = get(alice, "/api/tank/files/" + file_id)
    assert status == 200 and hashlib.sha256(ciphertext).hexdigest() == file_id
    assert ciphertext[:8] == b"TANKENC\0", "Fixture upload was not encrypted"
    assert get(bob, "/api/tank/files/" + file_id)[0] == 404
    assert get(bob, "/api/tank/registrations/" + file_id)[0] == 404
    while True:
        status, data = get(alice, "/api/tank/registrations/" + file_id)
        assert status == 200
        registration = json.loads(data)
        if MODE == "queued":
            assert registration["status"] == "pending", "Job was not durably queued before restart"
            assert registration["attempts"] == 0, "Stopped worker unexpectedly attempted registration"
            assert not registration.get("transaction_hash"), "Queued job already has a transaction"
            break
        if registration["status"] == "registered":
            transaction = registration["transaction_hash"]
            assert len(transaction) == 66 and transaction.startswith("0x")
            break
        assert registration["status"] != "failed", "Local registration failed"
        assert time.monotonic() < DEADLINE, "Local registration timed out"
        time.sleep(1)
    records.append({"file_id": file_id, "transaction_hash": registration.get("transaction_hash", ""),
                    "attempts": registration["attempts"]})

path = ROOT / "accepted-state.json"
queued = ROOT / "queued-state.json"
if MODE == "queued":
    with queued.open("x") as output:
        json.dump(ids, output)
elif MODE == "snapshot":
    assert ids == json.loads(queued.read_text()), "Restart lost or changed queued files"
    with path.open("x") as output:
        json.dump(records, output)
elif MODE == "verify":
    assert records == json.loads(path.read_text()), "Restart changed files or registration transactions"
print("PASS: local ciphertext IDs, registration state, and user isolation " + MODE)
