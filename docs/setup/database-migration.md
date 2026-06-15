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
password before deleting the local file.

## Unsupported Schema Error

If an older database is opened directly in the normal gateway, unlock fails with
an unsupported schema message. That is intentional. Create a fresh 0.2 database,
or run the migration helper above and unlock the new migrated database. If you
already migrated it and no longer need the old local copy, delete it from the
unlock screen with the old database password.

## Future Migrations

The helper is intentionally versioned. Future breaking preview-schema changes can
add another migration option, for example `0.2.x to 0.3.0`, without adding
runtime compatibility branches to the gateway.
