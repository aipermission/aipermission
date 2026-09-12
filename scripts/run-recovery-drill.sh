#!/bin/sh
set -eu

cd "$(dirname "$0")/../backend"
packages='./internal/api ./internal/db ./internal/legacymigration'
expected='TestRecoveryDrillEncryptedBackupWrongPasswordAndGatewaySecret
TestRecoveryDrillSelfHostedBackupDownloadAndRestart
TestRecoveryDrillSQLCipher442ApplicationFixtureMigratesViaProductionPath
TestRecoveryDrillLegacy010To020CopiesMinimumSSHConfiguration
TestRecoveryDrillLegacyMigrationCanRetryAfterSecretFailure'
discovered=$(go test $packages -list '^TestRecoveryDrill')
printf '%s\n' "$expected" | while IFS= read -r name; do
  printf '%s\n' "$discovered" | grep -qx "$name" || {
    printf 'required recovery drill is missing: %s\n' "$name" >&2
    exit 1
  }
done
count=$(printf '%s\n' "$discovered" | grep -c '^TestRecoveryDrill')
[ "$count" -eq 5 ] || {
  printf 'recovery drill discovered %s tests; expected exactly 5\n' "$count" >&2
  exit 1
}
go test $packages -run '^TestRecoveryDrill' -count=1
