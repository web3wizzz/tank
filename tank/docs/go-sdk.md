# Go SDK

The Go SDK provides authenticated access to the Tank coordinator.

Package: `tank.local/tank/pkg/tank`

This is the current repository-local module path. The SDK has not yet
been published under a public GitHub module path.

## Create a client

```go
client, err := tank.New(tank.Config{
    BaseURL: "http://127.0.0.1:8080",
    Token:   os.Getenv("TANK_API_TOKEN"),
    Timeout: 30 * time.Second,
})
if err != nil {
    return err
}
```

Import `tank.local/tank/pkg/tank` along with `os` and `time`.

Tokens are passed explicitly. The SDK does not read environment
variables automatically. A zero timeout defaults to two minutes.

## Methods

| Method | Result |
| --- | --- |
| `Health(ctx)` | Coordinator health check |
| `Tank(ctx, data)` | Manifest with a verified file ID |
| `Retrieve(ctx, fileID)` | Verified file bytes |
| `List(ctx, after)` | One page of up to 100 file IDs |
| `RegistrationStatus(ctx, fileID)` | Recorded registration status |

Pass an empty cursor to `List` for the first page. Use the last ID
from a full page as the cursor for the next page.

All methods accept a context for cancellation and deadlines.
Files must contain between 1 byte and 16 MiB.

## Error handling

Use `errors.As` with `*tank.APIError` to inspect an HTTP status code.

Use `errors.Is` with:

- `tank.ErrIntegrity`
- `tank.ErrInvalidFileID`
- `tank.ErrInvalidSize`

Redirects are not followed. Corrupted downloads return an error
without returning the unverified bytes.

## Run the complete example

Keep Anvil and the local Tank launcher running.

From the Go project directory:

```bash
set -a
source .env.tank-local
set +a
go run ./examples/go-sdk
```

The example stores fresh data, verifies exact retrieval, lists IDs,
and waits for automatic registration.

See [the example source](../examples/go-sdk/main.go).

## Registration boundaries

Successful storage does not imply completed registration.
Query `RegistrationStatus` separately.

A registered status reflects the worker's recorded verification.
The SDK does not independently query the blockchain.

Chain state resets can invalidate previously recorded statuses.
