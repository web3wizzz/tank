# Automatic registration

Tank stores files independently of chain availability.

The registration worker discovers saved manifests and queues their
commitments for the configured registry and registrant.

Run the worker from the Go project directory:

    set -a
    source .env.tank-local
    set +a
    ./bin/tank-registration-worker

The current worker supports local Anvil on chain ID 31337 and uses
an unlocked development account.

Authenticated status endpoint:

    GET /registrations/{file_id}

States: not_queued, pending, submitted, registered, failed.

RPC failures retry with increasing delays. Saved transaction hashes
allow processing to resume after a worker restart.

Verified locally:
- Automatic registration of a new file.
- Recovery after an RPC outage.
- Exact retrieval of the registered file.

Anvil state resets can invalidate earlier registration statuses.
Persistent chain state and revalidation remain follow-up work.
