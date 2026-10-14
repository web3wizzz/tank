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
BASE = "http://localhost:3330"


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
    status, ciphertext = get(alice, "/api/tank/files/" + file_id)
    assert status == 200 and hashlib.sha256(ciphertext).hexdigest() == file_id
    assert ciphertext[:8] == b"TANKENC\0", "Fixture upload was not encrypted"
    assert get(bob, "/api/tank/files/" + file_id)[0] == 404
    assert get(bob, "/api/tank/registrations/" + file_id)[0] == 404
    deadline = time.monotonic() + 90
    while True:
        status, data = get(alice, "/api/tank/registrations/" + file_id)
        assert status == 200
        registration = json.loads(data)
        if registration["status"] == "registered":
            transaction = registration["transaction_hash"]
            assert len(transaction) == 66 and transaction.startswith("0x")
            break
        assert registration["status"] != "failed", "Local registration failed"
        assert time.monotonic() < deadline, "Local registration timed out"
        time.sleep(1)
    records.append({"file_id": file_id, "transaction_hash": transaction})

path = ROOT / "accepted-state.json"
if MODE == "snapshot":
    with path.open("x") as output:
        json.dump(records, output)
elif MODE == "verify":
    assert records == json.loads(path.read_text()), "Restart changed files or registration transactions"
else:
    raise SystemExit("Expected snapshot or verify")
print("PASS: local ciphertext IDs, registered transactions, and user isolation " + MODE)
