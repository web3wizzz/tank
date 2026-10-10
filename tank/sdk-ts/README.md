# Tank TypeScript SDK

Store files, retrieve verified bytes, and check registration status
from Node.js applications.

## Current status

The SDK works locally and requires Node.js 24 or newer.

The package is private and has not been published to npm.
Use the built files from this repository.

## Build and test

From this directory:

```bash
npm ci
npm run check
npm test
```

The SDK uses built-in fetch and Web Crypto.
TypeScript is a development dependency.

## Create a client

From a JavaScript file in the examples directory:

```js
import { Tank } from "../dist/index.js";

const client = new Tank({
  baseURL: "http://127.0.0.1:8080",
  token: process.env.TANK_USER_TOKEN,
  timeoutMs: 30_000,
});
```

The default request timeout is two minutes.
Tokens must be passed explicitly.

## Methods

| Method | Result |
| --- | --- |
| `health(options?)` | Coordinator health check |
| `tank(bytes, options?)` | Manifest with a verified file ID |
| `retrieve(fileID, options?)` | Verified bytes as a Uint8Array |
| `list(after?, options?)` | One page of up to 100 file IDs |
| `fileInfo(fileID, options?)` | Authorized filename and byte size |
| `registrationStatus(fileID, options?)` | Recorded registration status |

File data must be a Uint8Array. Node.js Buffer values are accepted.

Files must contain between 1 byte and the configured upload limit (16 MiB by default).
File IDs are 64 lowercase hexadecimal characters, without 0x.

Response field names match the HTTP API, including file_id.

## Cancellation

Pass an AbortSignal through the request options:

```js
const controller = new AbortController();

const bytes = await client.retrieve(fileID, {
  signal: controller.signal,
});
```

Calling controller.abort() cancels the operation.

## Errors and verification

- APIError exposes the unsuccessful HTTP status as statusCode.
- IntegrityError identifies a content-hash mismatch.
- Invalid arguments and responses produce TypeError or RangeError.
- Response reads are bounded.
- Redirects are not followed.
- Retrieved bytes are returned only after hash verification.

Manifest validation checks its structure and uploaded file ID.
It does not independently recompute every returned segment Merkle root.

Registration status comes from the coordinator's recorded worker status.
The SDK does not independently query the blockchain.

## Run the local demo

Keep persistent Anvil and the Tank launcher running.

From this directory:

```bash
set -a
source ../.env.tank-local
set +a
npm run build
node examples/local-demo.mjs
```

The demo stores fresh data, verifies exact retrieval, lists file IDs,
and waits for automatic registration.

## Browser integration

The local Next.js workspace proxies authenticated requests through an HttpOnly
session. Browser encryption and the exact Codespaces HTTPS origin are verified.
Direct browser-to-coordinator CORS is not supported. Do not embed administrator
credentials in frontend code. See [browser encryption](../docs/browser-encryption.md).

## Credentials, privacy, and limits

Use a private individual user token (`tank_u_…`) for application clients.
The examples above expect `TANK_USER_TOKEN` to be supplied privately by your
application environment; launchers do not populate it. Never log it or expose it
in a public frontend. Administrator tokens access legacy/all files and manual
repair; node tokens are not coordinator credentials.

A user can list, inspect, retrieve, and check registration only for their files.
Another user's file returns 404. Credentials expire after 30 days; revocation
rejects subsequent requests. Renew with `tank-access issue --user-id USER_ID
--out NEW_PRIVATE_FILE` to preserve the same user's file access. Existing keys
remain valid until expiry or explicit revocation. See the
[credential guide](../docs/user-credentials.md).

SDK uploads send the supplied bytes without automatic encryption. Encrypt
sensitive input yourself or use the browser workspace; its recovery key is
separate from authorization and cannot replace a user token. A file ID is a hash
of the uploaded bytes, so browser IDs hash ciphertext.

Treat 413 as a file-size error, 429 as temporary pressure (honor `Retry-After`
and avoid unbounded automatic retries), and 507 as quota/capacity exhaustion.
Server deadlines can return 408/504; cancel through your context or AbortSignal.
Limits apply to principal identity across replacement credentials. See
[resource limits](../docs/resource-limits.md) and [security boundaries](../../SECURITY.md).

Pass `{ filename: "report.pdf", signal }` to `tank` to store a validated filename.
`fileInfo` returns it with the size. Filenames sent by SDK uploads are plaintext
metadata. `retrieve` returns bytes; your application chooses its output filename.

For complete pagination, request `list("")`, collect the returned IDs, and pass
the last ID of each full 100-item page to `list(after)` until a page is shorter
than 100. A concurrent upload whose ID sorts before the cursor may require a
fresh listing.
