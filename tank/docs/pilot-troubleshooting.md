# Pilot troubleshooting

Use a private terminal and the host's approved operator identity. Do not paste
command output containing environments, authenticated URLs, database paths,
tokens, cookies, signing files, or recovery keys into issues or chat. Public
transaction hashes and the registry address are recorded in
[Arc acceptance](arc-testnet-acceptance.md).

| Symptom | Check | Safe next step |
| --- | --- | --- |
| HTTPS endpoint does not resolve | `getent ahosts HOSTNAME` | Verify the selected DNS record and host firewall. Never replace HTTPS with public backend HTTP. |
| TLS fails | `curl --fail --silent --show-error --max-time 10 https://HOST/health` | Check certificate expiry, full chain, hostname, system time, and renewal service. Tank rejects invalid TLS and redirects. |
| Frontend returns 502 | `systemctl status tank-frontend tank-coordinator --no-pager` | Check service state and private service logs. Confirm the frontend has the exact HTTPS origin and the coordinator HTTPS URL in its private config. |
| Upload returns 413 or 507 | Review configured upload/logical limits and authenticated operational status | Reduce the test file or adjust an explicitly approved capacity policy. Check free bytes and inodes on every node; do not remove shards to clear quota. |
| New storage is unavailable while a node is down | Authenticated coordinator status and per-node health | New writes require all three initial placement nodes. Restore the affected node; spare repair does not silently change initial placement. |
| Retrieval is degraded | Authenticated status, repair backlog, node service state | Preserve node data. Restore the missing process/host and allow bounded maintenance repair to drain. Four valid shards are needed for reconstruction. |
| Registration is pending or failed | `/registrations/FILE_ID` as the owning user, coordinator aggregate status, `systemctl status tank-registration-worker.timer` | Check chain 5042002, the existing registry, RPC reachability, current testnet balance, and the unchanged fee/budget caps privately. Keep the worker journal and shared database intact. |
| Worker reports no completed transaction | Read-only worker pass and canonical public receipt/status | Do not delete a signed intent or create a parallel account/journal. Retry only through the existing guarded worker and permanent spend ledger. |
| Restore is required | Follow [backup and restore](backup-restore.md) | Pause public broadcasting first. Restore to a new private copy; reconcile post-snapshot signed/submitted transactions, nonce lane, and spend reservations before any broadcast resumes. |
| A user cannot retrieve a file | Confirm the correct user credential, file ID, and recovery-key file | Recovery keys are client-side secrets. Tank cannot recover a lost key; do not ask a user to send it to an operator. |

`/health` reports process liveness only. Use authenticated aggregate status and a
synthetic encrypted retrieval to assess service readiness. Keep the user's
independent copy until a retrieval succeeds. Local process-stop tests do not
establish independent-host recovery.
