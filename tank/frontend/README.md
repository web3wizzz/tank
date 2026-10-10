# Tank browser workspace

The Next.js workspace signs in with an individual Tank credential, encrypts files
with Web Crypto AES-256-GCM, and restores verified downloads with their original
filename. Recovery keys stay in the browser and the user's private downloads.

Use Node.js 24 or newer. From the parent `tank/` directory:

```bash
npm --prefix sdk-ts ci
npm --prefix sdk-ts run build
npm --prefix frontend ci
npm --prefix frontend run build
bash scripts/run-frontend.sh
```

Start `scripts/run-local.sh` separately for storage. Create an individual
credential with `tank-access`; follow the [repository setup](../../README.md#browser-workspace).
The launcher initializes missing local secrets without replacing existing ones.

In Codespaces, use the HTTPS port 3000 URL. The launcher persists the exact origin in `.env.tank-local`;
edit `TANK_FRONTEND_ORIGIN` there for another proxy or forwarded port. For local development,
export the session secret and `TANK_API_URL` from your private local environment
and use `npm run dev` with the matching origin.

From this directory:

```bash
npm test
npm run lint
npm run build
npx playwright install --with-deps chromium
npm run test:integration
```

`test:integration` starts its own temporary four-node storage stack and frontend,
creates two temporary user credentials, and checks encryption, filename recovery,
node failure, authorization, revocation, and logout. Existing services and files
are untouched. It requires Go and a production frontend build.

`test:browser` checks an existing running workspace using
`TANK_E2E_CREDENTIAL_FILE`. See the [encryption guide](../docs/browser-encryption.md)
for proxy transport options, file limits, key handling, and format details.
