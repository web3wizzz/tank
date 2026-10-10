# Local resource limits and quotas

Set these values in the private `.env.tank-local` used by the launchers, or in the
service environment. Restart the affected services after changing configuration.
Invalid values fail startup (Go) or return a configuration error (frontend).

| Setting | Default | Behavior |
| --- | --- | --- |
| `TANK_MAX_FILE_BYTES` | 16777216 | Maximum new upload body; 1–16 MiB supported |
| `TANK_MAX_CONCURRENT_REQUESTS` | 4 | Coordinator requests admitted before authentication |
| `TANK_MAX_CONCURRENT_PER_USER` | 2 | Concurrent authenticated requests per principal; at most the global limit |
| `TANK_REQUESTS_PER_MINUTE` | 300 | Per-principal token-bucket refill; 0 disables rate limiting |
| `TANK_REQUEST_BURST` | 60 | Maximum per-principal burst |
| `TANK_REQUEST_TIMEOUT_SECONDS` | 120 | Request deadline; 1–300 seconds |
| `TANK_MAX_HEADER_BYTES` | 16384 | Go server header bound; 1024–65536 bytes |
| `TANK_MAX_CONNECTIONS` | 64 | Accepted coordinator TCP connections, including idle/slow connections |
| `TANK_USER_STORAGE_BYTES` | 1073741824 | Logical committed bytes accessible to one user; 0 disables this quota |
| `TANK_TOTAL_STORAGE_BYTES` | 10737418240 | Total logical bytes in unique manifests; 0 disables this quota |
| `TANK_NODE_STORAGE_BYTES` | 10737418240 | Retained flat-directory file bytes per node; 0 disables this quota |
| `TANK_NODE_MAX_FILES` | 100000 | Retained regular files per node, bounding inode pressure; 0 disables this count limit |
| `TANK_NODE_MAX_CONCURRENT_REQUESTS` | 16 | Node requests, including rejected authentication attempts |
| `TANK_NODE_MAX_CONNECTIONS` | 64 | Accepted node TCP connections |
| `TANK_FRONTEND_MAX_CONCURRENT_REQUESTS` | 8 | Buffered frontend API/auth requests per frontend process |

Concurrency is nonblocking: saturated requests return **429** with `Retry-After`.
The coordinator accounts for a user by principal ID, so issuing a replacement
credential cannot reset that user's rate or concurrency budget. Unknown
credentials still consume the global authentication admission bound without
creating per-user buckets. Authenticated buckets remain in memory for the
administratively provisioned users encountered by this process. Coordinator/node health checks remain available.
Frontend logout remains available to terminate workspace operations under load.

Oversized uploads return **413**. Logical quota or node capacity failures return
**507**. Slow reads and requests have bounded deadlines. The frontend gives stable
messages for size, pressure, and storage-capacity errors instead of exposing raw
backend responses.

## Quota accounting and admission

The coordinator counts committed file bytes, not erasure-code overhead. A file
shared by two users counts once globally and once against each user's quota.
Uploading identical bytes again as the same user does not charge them again.
Administrator uploads bypass the per-user quota but retain the total storage,
request-size, rate, concurrency, and node-capacity bounds.

Before writing shards, an upload checks quotas under a short SQLite upload lease.
Uploads are serialized across coordinators sharing the database so simultaneous
requests cannot both pass the same quota check. Retrieval/listing can proceed in
parallel within their configured bounds. A bounded request context expires before
its lease; crashed admission owners expire automatically. A stale release cannot
remove a newer lease. A busy upload returns 429 rather than accumulating a queue.

Nodes enforce separate physical file-byte and file-count caps. Existing regular files, including
retained temporary files from earlier crashes, count toward capacity. Initialization
never deletes them. Writes and deletes are serialized within the node process;
use one node process per storage directory. Same-size shard repair at capacity is
allowed. Atomic replacement can temporarily require up to one additional shard of
space; filesystem metadata overhead is not part of the byte accounting, while the
file-count bound limits growth in retained directory entries.
Partial failed uploads may leave shards, which remain counted by the node cap.
Automatic garbage collection is not implemented.

Lowering upload or quota limits preserves existing data. Retrieval remains allowed;
new writes are rejected if the relevant capacity is exhausted. Limits are resource
bounds, not a guarantee against all denial-of-service attacks or disk failure.

The quota-admission table is an additive schema-6 migration. Upgrade coordinator,
worker, and local administrative tools together; older schema-5 tools cannot reopen
a schema-6 database. The migration retains manifests, filenames, users, credentials,
permissions, and durable jobs. Back up SQLite using its online backup mechanism
before upgrading an important evaluation database; do not copy a live WAL database
without its required state.

## Validation

Go tests cover invalid/disabled settings, global and per-user saturation, rate
refill and credential continuity, shared/duplicate quota accounting, cross-connection
upload leases, stale-release fencing, timeout handling, capacity persistence,
concurrent node writes, and preservation of existing shards. Frontend tests cover
configuration and idempotent admission release. The isolated browser stack uses
an explicit 8 KiB upload cap and disables rate limiting for its 101-file fixtures;
it checks oversized rejection while repeating the encryption/recovery/isolation
workflow. It strips inherited Tank settings to keep personal configuration out of
temporary integration services.
