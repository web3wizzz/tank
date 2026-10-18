# New-upload placement and failure domains

Each segment retains four data and two parity shards. New files probe all
configured nodes, including spares, and require three eligible failure domains.
Each selected node receives two shards per segment. Nodes are tried in configured
order; the original first three are preferred when healthy and sufficiently empty.
Existing manifests and ciphertext/raw-byte file IDs are unchanged by selection.

Configure explicit domains with `TANK_NODE_FAILURE_DOMAINS_FILE`, a bounded JSON
object mapping every exact configured URL to one nonempty stable label (up to
128 bytes), with no duplicate keys or extra/missing nodes. The
[pilot mapping template](../deploy/pilot/node-failure-domains.json.example) is an
example, not selected infrastructure. Map endpoints sharing a host/storage failure
boundary to the same label. The hosted template requires this file through
`TANK_REQUIRE_NODE_FAILURE_DOMAINS=true`; malformed boolean settings fail startup.
Apply mapping only at process startup, before serving requests.
Labels are exact, case-sensitive identifiers. Reuse the same spelling for shared
boundaries. Changing a label does not move existing shards or repair previous
collocation; inspect the accepted inventory and existing placements before changing
the mapping. Retrieval continues to use the original manifest locations.

For compatibility without an explicit mapping, remote domains default to canonical
hostnames, so different ports on the same hostname share a domain. Different DNS
names can still point to one machine: hostname fallback cannot prove independence.
Loopback fixtures default to separate endpoint labels for simulation only. Neither
these defaults nor operator labels establish actual distinct physical hosts.
Independent host inventory, access and failure tests remain hosted gates.

Eligibility checks observed byte and file headroom for the node's complete
two-shard share of the file, across all segments. These observations are not
distributed reservations. Node storage retains atomic quota enforcement when
uploads compete for the same capacity. If a node fails during a shard pair, both
indices are retried on an unused node/domain before recording that placement.
A failed endpoint does not disqualify another healthy endpoint in that domain
when no successful placement occupies it. No manifest is published until every
shard has been accepted. Failed partial writes can leave orphan shards; cleanup
is separate and must never delete shards referenced by surviving manifests.

Retrieval verifies shard hashes, reconstruction, Merkle roots, length and the
complete file digest. Repair verifies reconstructed bytes and replacement
readback before conditional location publication, and rejects replacement into
the domain of retained verified shards. Automatic repair follows the same
constraint. Authorization, per-user access and storage admission remain enforced
by the existing handlers. A healthy spare does not bypass user quotas or coding
thresholds. A four-node configuration with only two eligible domains cannot admit
new uploads even if three endpoints respond.

`/health` remains process liveness. Authenticated `/ops/status` upload readiness
counts available, non-full domains; actual admission also checks the requested
file's whole-file headroom. Readiness is not a capacity reservation, signing
permit, recovery activation proof or independent-host acceptance result.
