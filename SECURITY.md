# Security policy

Tank is a local development MVP. The current code has automated security
regressions and a focused development review, not an independent security audit
or a production durability guarantee. Keep original copies during evaluation.

## Reporting

Use this repository's private vulnerability reporting feature if available.
Otherwise contact the maintainer through a published private contact method on
the repository owner's GitHub profile. Do not post exploit details or private
data publicly while arranging disclosure. Include the affected commit, expected
and actual behavior, and a minimal reproduction using synthetic data. Redact all
credentials, recovery keys, private keys, RPC credentials, and file contents.

## Trust boundaries

Individual credentials authorize one principal's files. Lists, metadata,
retrieval, and registration status are scoped; unauthorized file IDs return 404.
Administrators can access legacy and all files and repair storage. Provisioning,
renewal, and revocation are local administrative commands. Node credentials grant
internal shard access and belong only on trusted services. These services use
HTTP locally; remote transport security and public-network operation are outside
the supported MVP. Do not expose the coordinator or node ports publicly.

The browser encrypts file contents and the original filename with AES-256-GCM
using a fresh random key and IV per file. The key is downloaded to a recovery JSON
file and never sent to Tank. Metadata still reveals ciphertext size, content ID,
shard placement, and an ID-based storage filename. SDK and CLI input is stored
without automatic encryption. Keys do not bypass user authorization. A lost key
cannot be recovered by Tank; anyone with both ciphertext and its key can decrypt.
Protect downloaded keys and remove them from shared browser downloads yourself.

Browser sessions use authenticated encryption in a Secure cookie on HTTPS,
HttpOnly, SameSite=Lax, with an eight-hour lifetime. The exact frontend origin and
workspace request marker are checked, including constrained Codespaces forwarding.
Each storage request rechecks backend credential validity. Logout removes this
browser's cookie and aborts workspace operations; a previously copied session
cookie can be replayed until expiry unless its underlying credential is revoked.
Rotate the session secret to invalidate every session. Revocation is the way to
invalidate a compromised individual credential. Frontend same-origin script
compromise or a malicious browser extension can read plaintext and recovery keys.

Shard and file hashes verify content integrity; they do not prove indefinite
availability or independent operator durability. Registration is an optional
commitment on local Anvil with a public development account, not private content
storage or per-user on-chain ownership. Recorded job status is not continuously
revalidated after a chain reset. Public-chain signing is unsupported.

## Resource and local-state handling

Request limits, rate/concurrency bounds, logical quotas, and physical node caps
are documented in [resource limits](tank/docs/resource-limits.md). They preserve
existing data but do not guarantee protection from all denial of service or disk
failure. Failed uploads may retain shards; automatic garbage collection is not
implemented. Use one node process per storage directory and upgrade all metadata
users together for schema migrations.

Never commit environment files, credentials, recovery files, SQLite/WAL state,
stored files, chain snapshots, logs, or build artifacts. Launchers and credential
tools create private files and refuse unsafe overwrites. Keep private backups,
avoid shell tracing around secrets, and never attach them to CI or issues.
The isolated integration suite uses synthetic data and removes only its own
new temporary stack. It does not modify the user's live nodes or database.

The maintained evaluation target is the current working branch and the Node/Go
versions pinned in repository manifests. No supported released version or public
package distribution has been established.
