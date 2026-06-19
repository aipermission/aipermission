# Google Drive Backup Provider

AIPermission can store encrypted `.aipdb` database backups in Google Drive.
Google Drive is only a remote storage provider for already encrypted backup
files. It is not a remote gateway, it does not receive MCP tokens, connector
credentials, SSH keys, or database passwords, and it cannot decrypt a database.

This guide explains how to create the Google OAuth credentials required by the
Settings page.

## What You Need

- A Google account.
- A Google Cloud project.
- Google Drive API enabled for that project.
- One OAuth client configured for TVs and limited-input devices.

AIPermission needs two values from that OAuth client:

- OAuth client ID
- OAuth client secret

The client ID is stored as provider metadata. The client secret and the Google
tokens returned after authorization are stored only as encrypted provider
secrets inside the local SQLCipher database. Provider list/detail API responses
never return the secret or token payload.

## Create The OAuth Client

1. Open Google Cloud Console.
2. Select an existing project or create a new project for AIPermission backups.
3. Go to `APIs & Services` > `Library`.
4. Search for `Google Drive API`.
5. Open it and click `Enable`.
6. Go to `APIs & Services` > `OAuth consent screen`.
7. Configure the consent screen enough for your account to authorize the app.
8. If the app is in testing mode, add your own Google account as a test user.
9. Go to `APIs & Services` > `Credentials`.
10. Click `Create credentials` > `OAuth client ID`.
11. Choose `TVs and Limited Input devices` as the application type.
12. Name the client, for example `AIPermission Local Backup`.
13. Create it and copy the generated client ID and client secret.

Official Google documentation:

- [OAuth 2.0 for TV and Limited-Input Device Applications](https://developers.google.com/identity/protocols/oauth2/limited-input-device)
- [Create access credentials](https://developers.google.com/workspace/guides/create-credentials)

## Configure AIPermission

1. Open AIPermission.
2. Unlock the local database.
3. Go to `Settings`.
4. In `Remote backup providers`, add or edit a `Google Drive` provider.
5. Enter a provider name.
6. Enter the Google Drive folder name for encrypted backups.
7. Paste the OAuth client ID.
8. Paste the OAuth client secret.
9. Save the provider.
10. Click `Connect` on the provider row.
11. AIPermission shows a user code and a Google verification link.
12. Open the Google verification link.
13. Enter the code and approve access.
14. Return to AIPermission and click `Finish connection`.

After the connection succeeds, the provider row should show that Google Drive is
connected.

## Upload A Backup

1. Make sure the Google Drive provider shows `Google Drive connected`.
2. Click `Upload` on the provider row.
3. AIPermission creates a temporary SQLCipher snapshot of the currently unlocked
   database.
4. The encrypted `.aipdb` file is uploaded to the configured Google Drive
   folder.
5. A local backup record is saved with the Drive file id, filename, size,
   checksum, source machine, and timestamps.

The uploaded file is encrypted with the database password. Google Drive stores
the encrypted blob only.

## Restore Or Download A Backup

1. Make sure the Google Drive provider shows `Google Drive connected`.
2. Click `Backups` on the provider row.
3. Choose one of the recorded backups.
4. Click `Download` to save the encrypted `.aipdb` file locally, or click
   `Restore` to import it as a new local database.
5. For restore, enter a new local database name and the database password that
   protected the backup when it was uploaded.

Restore never overwrites the currently open database. AIPermission downloads the
remote encrypted `.aipdb`, verifies the stored size/checksum metadata, validates
the password, creates a new local database, and unlocks that new database.

## Troubleshooting

### Connect Says A Client ID Or Secret Is Missing

Edit the provider and fill both OAuth fields:

- Google OAuth client ID
- Google OAuth client secret

The secret field is blank when editing an existing provider because secrets are
not returned by the API. Leave it blank to keep the existing secret, or paste a
new value to replace it.

### Google Says The Client Type Is Wrong

Create the OAuth client as `TVs and Limited Input devices`. Other OAuth client
types can fail the device authorization flow.

### Google Shows An Unverified App Warning

For personal/local use, keep the OAuth app in testing mode and add your own
Google account as a test user. If you want other Google accounts to use the same
OAuth app, follow Google's verification requirements.

### Authorization Keeps Waiting

Approve the code in the Google browser tab first, then return to AIPermission and
click `Finish connection`. If the code expired, start the connection again.

### Upload Says The Provider Is Not Connected

Reconnect the Google Drive provider. AIPermission needs a stored access token or
refresh token before it can upload encrypted backups.

### Restore Says The Password Is Invalid

Backups open with the database password that was active when the backup was
created. If you changed the local database password after uploading a backup,
use the old password for that backup.

## Security Notes

- Google Drive stores encrypted `.aipdb` backup files only.
- The database password is never uploaded.
- OAuth tokens are encrypted in the local database.
- OAuth tokens are not exposed to MCP.
- Backup provider setup is local UI-only.
- Remote backup providers do not change AIPermission's local-only gateway model.
