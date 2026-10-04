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
  token: process.env.TANK_API_TOKEN,
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
| `registrationStatus(fileID, options?)` | Recorded registration status |

File data must be a Uint8Array. Node.js Buffer values are accepted.

Files must contain between 1 byte and 16 MiB.
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

Browser deployment and coordinator CORS configuration have not been
validated. Do not embed the shared development API token in a public
frontend.
