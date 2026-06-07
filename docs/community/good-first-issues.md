# Good First Issue Pool

This pool is a starter list for GitHub issues after the first public RC. Keep
issues small, scoped, and friendly to contributors who are learning the codebase.

## Documentation

- Add screenshots to README after final visual polish.
- Add one short example for each MCP tool response.
- Improve provider-specific MCP setup notes as client docs change.
- Add a short troubleshooting note for "MCP is stopped" responses.

## UI Polish

- Improve empty states on History, Audit Logs, and Messages.
- Add small keyboard-accessibility tests for Dialog and Drawer components.
- Add copy-feedback coverage for install commands and backup confirmation names.
- Add a compact mobile pass for Settings and Security pages.

## Tests

- Add Playwright coverage for Settings security toggles.
- Add Playwright coverage for import flow using a temporary encrypted database.
- Add Playwright coverage for token permission edits from Console and Tokens.
- Add backend tests for more retention combinations.

## Security And Safety

- Add short examples that explain when to use temporary permission grants.
- Add a command policy prototype that warns, but does not block, common high-risk commands.
- Add more built-in redaction patterns with focused tests.

## Backup And Import UX

- Add clearer import failure messages for wrong database passwords.
- Add a post-import checklist that reminds users host key trust is local machine state.
- Add a restore smoke test note for contributors.
