# Database Migration

AIPermission 0.2 starts from a clean connector-native database baseline. The
normal gateway does not carry long-term compatibility code for pre-0.2 preview
databases.

If you need to keep important 0.1.x local data, use the versioned migration
helper. It is a separate local-only Compose service that creates a new 0.2
database and never modifies the source database.

## 0.1.x To 0.2.0

Start the migration helper only when you need it:

```bash
docker compose --profile migrate up -d --build migration
```

Open:

```txt
http://localhost:3211
```

Do not use the source database in the normal gateway while migration is running.
For the cleanest copy, lock the source database or stop the normal gateway
containers before starting the migration.

The migration page asks for:

- the source 0.1.x database
- the source database password
- the new 0.2 database name
- the new 0.2 database password

The source password can be an older 0.1.x password. The new 0.2 database
password follows the current password rule: at least 14 characters with
uppercase letters, lowercase letters, and numbers.

The helper migrates:

- SSH keys into connector credential resources
- SSH servers into SSH connector targets
- SSH username/key bindings into credential profiles
- API tokens
- existing SSH `exec` token permissions
- settings, gateway secret, redaction rules, and history labels

It intentionally does not migrate:

- command history
- audit logs
- console sessions
- file transfer history
- 0.2-only connector activity records

After the migration succeeds, stop the helper:

```bash
docker compose --profile migrate stop migration
docker compose --profile migrate rm -f migration
```

Then return to the normal gateway at:

```txt
http://localhost:3210
```

The source 0.1.x database remains in the local database list until you remove
it. That is intentional: the helper never deletes or edits source data. After
you verify the migrated 0.2 database, select the old database on the unlock
screen and use **Delete old local copy**. AIPermission asks for that database
password in the normal unlock form, then asks you to type the database name
before deleting the local file.

## Gateway Secret Recovery

Connector credentials in a legacy database may depend on the gateway secret
that encrypted them. Most supported source databases carry that secret inside
their encrypted `settings` table. Older preview databases may instead require
the original `gateway.secret` file from the source data directory, or the exact
`AIPERMISSION_GATEWAY_SECRET` value that was used with that installation.

If migration reports a missing gateway secret or cannot decrypt a credential:

1. Stop the migration helper.
2. Restore the original source data directory and its `gateway.secret`, or set
   the original explicit `AIPERMISSION_GATEWAY_SECRET` value.
3. Start the helper and retry with the same source database.

Do not generate or rotate a gateway secret to work around this error. A new
secret cannot decrypt payloads protected by the old one. If the original secret
is irretrievably lost and is not embedded in the encrypted source database,
those encrypted connector payloads cannot be recovered. The source database
still remains unchanged.

Treat the gateway secret as sensitive. Never paste it into an issue, log,
diagnostics bundle, or migration screenshot.

## Interrupted Or Failed Migration

Migration writes to a hidden staging database beside the requested target. The
source remains read-only, and the final target name is published without
overwriting only after the staged database is complete, validated, and closed.
Normal failures remove the staging file, so correcting the cause and retrying
the same target name is safe while no final target exists.

An abrupt process or host shutdown can leave a hidden file whose name contains
`.migration-`. Stop the migration helper before removing only that abandoned
staging artifact. Do not remove the source database or a completed target. If a
final target with the requested name already exists, verify that database or
choose a different target name; the helper intentionally will not replace it.

Use this recovery order:

1. Preserve the source database and original gateway-secret material.
2. Stop the helper and correct the reported password, secret, storage, or input
   problem.
3. Confirm that no completed target with the requested name exists.
4. Restart the helper and retry.
5. Unlock and inspect the migrated target before deleting any old local copy.

## Unsupported Schema Error

If an older database is opened directly in the normal gateway, unlock fails with
an unsupported schema message. That is intentional. Create a fresh 0.2 database,
or run the migration helper above and unlock the new migrated database. If you
already migrated it and no longer need the old local copy, delete it from the
unlock screen by entering the old database password in the unlock form and
confirming the database name.

## Future Migrations

The helper is intentionally versioned. Future breaking preview-schema changes can
add another migration option, for example `0.2.x to 0.3.0`, without adding
runtime compatibility branches to the gateway.
