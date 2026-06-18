# @aipermission/mcp

Local-first MCP bridge for the AIPermission connector gateway.

AIPermission lets AI coding assistants use scoped connector actions through a
local gateway without receiving SSH private keys, database passwords, API
credentials, or other connector secrets.

The gateway is intentionally local-only. Run it on the developer machine and
keep the URL on `localhost`; remote systems are connector targets, not places
to host the gateway for LAN or internet users. SSH, Postgres, and Redis are
built-in connectors that use the same target/profile/action permission model
as future connectors.

![AIPermission demo: AI operates through approval-based connector access](https://raw.githubusercontent.com/aipermission/aipermission/main/docs/assets/demo/aipermission-demo.gif)

[Watch the demo video](https://github.com/aipermission/aipermission/releases/download/v0.1.0-rc.1/aipermission-demo.mp4) to see an AI assistant install Uptime Kuma on a VPS while the user approves commands and changes the plan mid-run.

`@aipermission/mcp` is the official MCP bridge package. The unscoped `aipermission` npm package is only a placeholder that points users here.

The package includes MCP Registry metadata:

- `mcpName` in `package.json`
- `server.json` with the npm stdio package declaration

## Install

```bash
npx -y @aipermission/mcp init \
  --provider codex \
  --name aipermission
```

The init command prompts for your AIPermission API token and writes the MCP client configuration for the selected provider.

The generated MCP config contains a bearer token. Keep it private. For project-local configs such as `.mcp.json`, `.cursor/mcp.json`, and `.vscode/mcp.json`, the init command refuses to write into files already tracked by Git unless `--force` is passed. For untracked project-local configs, it adds the file to `.git/info/exclude` when it detects a Git repository. Use `--print` if you prefer to copy the config manually. If a token config is committed or shared, revoke that token in the AIPermission UI.

## Manual Config

```json
{
  "mcpServers": {
    "aipermission": {
      "command": "npx",
      "args": ["-y", "@aipermission/mcp"],
      "env": {
        "NODE_ENV": "production",
        "AIPERMISSION_API_URL": "http://localhost:3210",
        "AIPERMISSION_API_TOKEN": "YOUR_TOKEN_HERE"
      }
    }
  }
}
```

## Tools

- `list_connector_targets`
- `get_connector_help`
- `get_connector_actions`
- `call_connector_action`
- `get_connector_action_request`

All integration work goes through connector targets. SSH, Postgres, Redis, and future
connectors share the same model: target, credential profile, connector action,
token action permission, approval, history, and audit.

For SSH, call `get_connector_actions(target_ref)` to discover actions such as
`exec`, `read_console`, `restart_console_session`, `browse_remote_files`, and
`start_file_download`. SSH `exec` is intended for non-interactive commands. Use
the web console for truly interactive work.

For Postgres, call `get_connector_actions(target_ref)` to discover schema,
table, and bounded read-only query actions. Postgres targets can connect
directly from the gateway or over an SSH connector profile when the database is
reachable only from a remote server.

For Redis, call `get_connector_actions(target_ref)` to discover bounded key
browser actions such as `scan_keys`, `get_key`, `set_string`, `expire_key`, and
`delete_keys`.

Connector responses can include `approval_pending` or `running`. Poll
`get_connector_action_request(request_id)` until the request reaches a terminal
status. MCP tool responses never include file contents, gateway temporary paths,
archive staging paths, or local upload contents.

## Operator Skill

Install the optional AIPermission operator instructions for your AI client:

```bash
npx -y @aipermission/mcp install-skill --client codex
```

Supported clients:

- `codex`: `~/.codex/skills/aipermission-operator/SKILL.md`
- `claude-code`: `.claude/rules/aipermission-operator.md`
- `cursor`: `.cursor/rules/aipermission-operator.mdc`
- `vscode`: `.github/instructions/aipermission-operator.instructions.md`
- `windsurf`: `.windsurf/rules/aipermission-operator.md`
- `antigravity`: `.agents/rules/aipermission-operator.md`
- `gemini`: `GEMINI.md`
- `custom`: prints portable Markdown to stdout

These instructions teach the agent how to discover connector targets, poll
`approval_pending` and `running` connector action requests, handle `stale`
approvals by sending a fresh request, write short reasons, use explicit file
transfer paths, and avoid printing secrets. The default installer uses the
operator instruction bundled in the npm package; `--source` accepts local file
paths only and rejects HTTP(S) sources.

## Security Boundary

This package talks to a local AIPermission gateway. `AIPERMISSION_API_URL` must
point to `localhost`, `127.0.0.1`, or `[::1]`; remote URLs are rejected before
the bearer token is sent. Do not expose the gateway on LAN or the public
internet, and do not use it as a shared DevOps service. Tokens grant access only
to connector targets, credential profiles, and action rules configured in the
gateway UI. Connector permissions may be temporary; expired grants are omitted
from `list_connector_targets` and no longer authorize connector actions. Target
visibility is permission-scoped, not a live health check; treat action execution
errors as the current reachability signal.

## License

AGPL-3.0-only from v0.1.14 onward.

Versions up to and including v0.1.13 were released under MIT and remain
available under their original MIT license.
