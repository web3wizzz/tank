# Individual user credentials

Tank uses a local administrative tool to create users, issue replacement
credentials, inspect credential metadata, and revoke access. These operations
require direct access to the coordinator's SQLite database. They are not exposed
as public signup or account-management endpoints.

From `tank/`, build the tool:

```bash
go build -o bin/tank-access ./cmd/tank-access
```

The default database is `data/local-demo/tank.sqlite`. Set `TANK_DATABASE_PATH`
or pass `--db PATH` for another database. Use the same database as the coordinator.

## Create a new user

```bash
./bin/tank-access create --label "Alice" --out data/local-demo/alice-credential.json
```

This creates a new identity and its first 30-day credential. The raw token is
written only to the new mode-0600 private file. Existing destinations and symlinks
are refused. The database stores a SHA-256 token hash, not the raw token.

Creating another user with the same label does not preserve file permissions;
labels are not identity identifiers.

## Inspect users and credentials

```bash
./bin/tank-access users
./bin/tank-access keys --user-id USER_ID
```

These local administrative commands emit JSON metadata. User listings include
IDs and labels. Credential listings include credential IDs, the user ID, expiry,
and revocation state. They never return a token or token hash. Expired credentials
are visible so administrators can inspect the user's credential history.

## Renew access for an existing user

```bash
./bin/tank-access issue --user-id USER_ID --out data/local-demo/alice-replacement-credential.json
```

Use the `principal_id` from the original private credential file or the ID from
`users`. A replacement has a fresh secret and another 30-day expiry, but retains
the same identity and all existing file access, filenames, and encrypted storage.
An administrator can issue it even if the user's previous credentials have all
expired or been revoked. Unknown user IDs are rejected.

Existing credentials continue to work until they expire or are explicitly
revoked. This allows you to save and test a replacement before disabling the
original. Failed issuance and refused output destinations do not revoke existing
credentials. If writing a newly issued credential fails, the tool attempts to
revoke only that newly created credential.

Sign out of the workspace, then sign in using the replacement file's token.
Import the existing file recovery key to decrypt an older encrypted file. Issuing
a new account credential does not replace or recover a file encryption key.

After verifying the replacement, revoke the original credential using its `id`:

```bash
./bin/tank-access revoke --key-id OLD_CREDENTIAL_ID
```

A revoked credential cannot authenticate again. An existing browser session
using it fails on its next authenticated request. Revocation preserves the user's
files and permits their other active credentials to keep accessing those files.

Store credential and recovery files privately. Keep credentials under the ignored
`data/` directory or another private location outside the checkout. Never paste
raw tokens into command-line arguments, logs, issues, or commits.
