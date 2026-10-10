# Pilot operational monitoring

`GET /health` remains a cheap process-liveness check. It does not certify shard
availability, disk capacity, database health, or completed chain registration.

`GET /ops/status` on the coordinator requires the administrator credential.
Individual user credentials are rejected. It returns non-cacheable aggregate
JSON with:

- `upload_ready`: metadata is readable, all three initial-placement nodes report
  usable capacity, and the configured logical total quota is not exhausted.
- `degraded`: a configured node is unavailable or a repair job remains outstanding.
- `metadata`: file count, committed bytes, repair jobs, and registration job counts
  by pending/submitted/failed/registered state.
- `logical_byte_limit`: the configured logical cap (zero means disabled).
- `nodes`: stable indices ordered by configured URL, initial-placement role,
  availability, and retained bytes/files with their caps. URLs and shards are
  omitted; compare indices to the operator's private inventory.
- `last_audit`: never/completed/failed and the most recent completion time. This
  tracks execution, not a proof that every file is healthy. It resets on restart.

Storage-node `GET /ops/status` requires that node's internal credential and
returns only aggregate retained byte/file capacity. It checks the storage root;
a failed backend returns 503 while liveness can remain 200. Remote checks are
bounded by a two-second coordinator deadline and at most four probe workers.
No filenames, file IDs, principal IDs, secrets, or raw worker errors are included.

Poll approximately every 30 seconds using a private monitoring credential source.
Do not put credentials in URLs, logs, command traces, dashboard links, or metric
labels. Alert on degraded status, `upload_ready=false`, rising repair/failed
registration counts, stale/failed audits, and capacity approaching configured
caps. Also monitor host free disk/inodes, process restarts, TLS expiry, network
availability, backup age, and measured restore success outside Tank.

HTTP 200 means the report was obtained; inspect its fields to judge readiness.
New uploads currently require the original three placement nodes. Degraded
retrieval can succeed from four verified shards, and automatic repair can move
stored shards onto an unused spare; that does not automatically replace the
initial placement list for new uploads. Readiness cannot guarantee that every
stored file is retrievable: use synthetic encrypted upload/retrieval drills.
Physical filesystem exhaustion can occur before a configured node quota, so
capacity reporting is not a substitute for host disk monitoring.
