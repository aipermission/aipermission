# Projects

Projects organize connector targets inside one local AIPermission database.
They are folders and MCP visibility scopes for a single developer; they are not
users, teams, tenants, or remote RBAC boundaries.

Examples:

- `My Project`
- `Client Infrastructure`
- `Personal infrastructure`

Every connector target belongs to exactly one project. Existing targets are
placed in the protected `Ungrouped` project when the database is upgraded.

## What Projects Control

Projects provide two related behaviors:

1. The Connectors and Console target lists are grouped by project.
2. Each API token can enable or disable visibility for each project.

Project visibility is an additional MCP boundary above connector action
permissions. An MCP token can use an action only when both conditions are true:

```txt
project is enabled for token
AND
target/profile/action grant is effective
```

Disabling a project for a token:

- removes that project's target/profile refs from `list_connector_targets`
- blocks direct connector action calls for those refs
- preserves the underlying action grants
- does not stop another token that still has access
- does not lock, delete, or disconnect the connector targets

Re-enabling the project restores the preserved grants, subject to their normal
token validity, expiration, and execution rules.

## Defaults

- `Ungrouped` always exists and cannot be archived.
- New connector targets must be assigned to one project.
- Existing targets migrate to `Ungrouped`.
- New tokens start with all current projects enabled.
- New projects start enabled for existing tokens.
- Enabling a project does not create connector action grants. A token still
  needs explicit target/profile/action permissions.

These defaults preserve existing local workflows while action grants remain
the authority for what a token may execute.

## Project Lifecycle

Project names can be changed. The stable slug does not change when the display
name changes, so audit and integration identity does not drift with UI copy.

A project can be archived only after all active connector targets have been
moved to another project. Historical activity remains associated with the
project snapshot recorded when the activity happened.

History and Audit Logs can be filtered by project. Moving a connector target to
another project does not rewrite its older history or audit entries.

## Local-Only Boundary

Projects do not change AIPermission's trust model. The gateway remains a
local-only, single-user developer tool. Projects organize one developer's
connector workspace and reduce what each local MCP token can discover and use.
They do not provide multi-user isolation suitable for LAN or hosted use.

## Connector Contract

Project ownership belongs to the generic connector target layer. Connector
implementations receive and preserve project assignment through shared target
save APIs; they must not create project-specific tables, permission checks,
history streams, MCP tools, or frontend project controls.

Adding a normal connector should require no changes to project persistence or
token project-scope enforcement.
