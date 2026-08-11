# GitHub Labels

Use a small, stable label set. Labels should help triage without
turning the project into process theater.

| Label | Purpose |
| --- | --- |
| `bug` | Something is broken or behaves incorrectly. |
| `enhancement` | Product improvement that fits the project principles. |
| `documentation` | README, docs, examples, or setup guidance. |
| `security review` | Security-sensitive review, hardening, or threat-model discussion. |
| `local-only` | Issues touching the localhost-only boundary. |
| `question` | Support or clarification request. |
| `wontfix` | Intentionally out of scope or conflicts with project principles. |
| `duplicate` | Already tracked elsewhere. |
| `good first issue` | Small, low-risk contribution for new contributors. |
| `help wanted` | Maintainers welcome community implementation help. |
| `maintenance` | Refactor, cleanup, dependency, CI, or release chores. |
| `ui` | Frontend/user-experience changes. |
| `mcp` | MCP package, client setup, or MCP protocol behavior. |
| `backend` | Go backend/API/storage/execution changes. |
| `frontend` | React frontend changes. |
| `docs` | Documentation-only work. |

## `wontfix` Policy

Use `wontfix` for requests that conflict with AIPermission's core identity:

- hosted SaaS mode,
- multi-tenant architecture,
- remote gateway hosting,
- shared team deployments,
- LAN-accessible gateway mode,
- cloud-managed execution.

Suggested response:

```text
Closed as wontfix.
Conflicts with AIPermission project principles:
local-only, single-user, developer-focused, human-in-the-loop.
```
