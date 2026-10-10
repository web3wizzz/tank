# Contributing to Tank

Thanks for helping build Tank.

We welcome documentation improvements, reproducible bug reports, meaningful tests, and focused changes to the local storage MVP.

## Find a useful task

Good starting points include:

- Fixing a confusing setup step or error message.
- Reporting a bug with enough detail to reproduce it.
- Adding coverage for a failure path, such as a stale lease or RPC interruption.
- Improving registration status handling or chain reset detection.
- Building a repeatable end-to-end demo.

Open an issue before starting a large feature, protocol change, new dependency, or public-network integration.

Explain the user problem and proposed behavior so maintainers can discuss scope before you invest substantial time.

## Set up locally

Start from a checkout of the repository.

The Go project is in the nested `tank/` directory.

Use the Go toolchain specified in `tank/go.mod`.

Bash, curl, and OpenSSL are needed for the storage launcher. Foundry is needed for contract work and local registration.

From the repository root:

```bash
cd tank
bash scripts/run-local.sh
```

For blockchain work, start persistent Anvil in a separate terminal from the Go project directory:

```bash
bash scripts/run-anvil.sh
```

Follow the [README](README.md#optional-enable-local-blockchain-registration) for deployment and chain configuration.

Keep local tokens, databases, snapshots, and test file contents out of commits.

## Make a change

Create a branch from your current checkout:

```bash
git switch -c fix/describe-your-change
```

Keep pull requests focused.

Match the surrounding code and format changed Go files with `gofmt`.

Explain behavior and tradeoffs when they are not obvious from the code.

For behavior changes, add a test that catches the original problem or demonstrates the new behavior.

Documentation-only changes do not require unrelated tests.

### Properties to preserve

- Encoding must not mutate caller-owned input.
- Retrieved bytes must match the file ID.
- Reconstructed shards must match the manifest.
- Repair can change locations, but must preserve the file commitment.
- Metadata migrations must preserve existing records.
- Job leases must prevent stale workers from completing another worker’s job.
- Chain outages must not prevent independent file storage and retrieval.
- On-chain commitment conflicts must not be silently overwritten.
- Storage paths and outbound node requests must remain validated.
- User file lists, metadata, retrieval, and registration must remain scoped.
- Browser plaintext, original encrypted filenames, and recovery keys stay client-side.
- Resource limits and migrations preserve existing data and credential identity.

If your change affects one of these properties, describe how you verified it.

## Run checks

From the Go project directory:

```bash
go test -race -count=1 -timeout=5m ./...
go vet ./...
go build ./cmd/...
```

For Solidity changes, from `tank/contracts/`:

```bash
"$HOME/.foundry/bin/forge" fmt --check
"$HOME/.foundry/bin/forge" test
```

For launcher changes, from the Go project directory:

```bash
bash -n scripts/*.sh
python3 scripts/test-local-env.py
python3 scripts/test-pilot-templates.py
python3 scripts/test-base-sepolia.py
```

For SDK changes, from `tank/sdk-ts/`:

```bash
npm ci
npm run check
npm test
```

For browser workspace changes, install dependencies and build the linked SDK first:

```bash
cd tank
npm --prefix sdk-ts ci
npm --prefix sdk-ts run build
npm --prefix frontend ci
```

Then from `tank/frontend/`:

```bash
npm test
npm run lint
npm run build
npx playwright install --with-deps chromium
npm run test:integration
# Extended local pilot acceptance used by CI:
npm run test:pilot
```

The integration runner creates temporary users, nodes, and metadata. It verifies
browser encryption, original filenames, user isolation, revocation, and retrieval
after one test node stops, and removes its own data at exit. It uses a local proxy
simulation at an HTTPS browser origin; it does not log in to GitHub's Codespaces
gateway. See [local MVP checks](tank/docs/local-mvp.md).

Before committing:

```bash
git diff --check
git status --short
```

Review `git diff --cached` and stage only the intended milestone files. Verify that
`.env*`, credential JSON, recovery key JSON, databases, `tank/data/`, logs,
`node_modules/`, `.next/`, SDK `dist/`, Go `bin/`, and contract `out/cache/broadcast`
remain ignored and untracked. Never include secret values in issues, test output,
CI artifacts, screenshots, or commits. Preserve unrelated edits and local data.

Check the repository’s CI workflow for any additional required checks.

## Open a pull request

Include:

1. The problem and the behavior your change produces.
2. A related issue, if one exists.
3. Checks you ran and their results.
4. Any migration, configuration, or compatibility changes.

A useful description explains a concrete result:

> Improved startup errors when a required chain setting is missing. Added coverage for incomplete configuration and verified that storage-only startup still works.

This is an example description, not a claim about an existing change.

Update documentation when routes, commands, configuration, or guarantees change.

Use small, reviewable commits.

A maintainer will review the change. Passing CI alone does not guarantee acceptance.

## Report a bug

Include:

- The relevant commit.
- Go and Foundry versions, where relevant.
- Commands you ran.
- Expected result.
- Actual result.
- A minimal reproduction.

For node or chain failures, describe which process stopped and whether state was preserved.

Redact tokens, RPC credentials, private keys, and private file contents from logs.

## Report a security issue

Follow [SECURITY.md](SECURITY.md). For a suspected security vulnerability, use the repository’s private security reporting feature if enabled.

Otherwise, contact the maintainer privately through a published contact method before posting exploit details publicly.

## Keep discussions useful

Be respectful, explain disagreements with evidence, and help others reproduce your findings.

Documentation should distinguish working features from planned ones and avoid implying guarantees the implementation has not established.
