# @aipermission/mcp

Local-first MCP bridge for the AIPermission gateway.

AIPermission lets AI coding assistants use scoped server access through a local gateway without receiving SSH private keys or server credentials.

The gateway is intentionally local-only. Run it on the developer machine and keep the URL on `localhost`; remote servers are SSH targets, not places to host the gateway for LAN or internet users.

![AIPermission demo: AI installs Uptime Kuma through approval-based SSH access](https://raw.githubusercontent.com/aipermission/aipermission/main/docs/assets/demo/aipermission-demo.gif)

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

- `list_servers`
- `exec`
- `get_request`
- `list_requests`
- `read_console`
- `restart_console_session`
- `send_message`
- `list_file_transfers`
- `get_file_transfer`
- `list_file_transfer_batches`
- `get_file_transfer_batch`
- `browse_remote_files`
- `start_file_download`
- `save_file_download`
- `upload_files`
- `pause_file_transfer_batch`
- `resume_file_transfer_batch`
- `cancel_file_transfer_batch`

`exec` is intended for non-interactive commands. The gateway closes stdin for MCP command bodies so stdin-reading commands cannot consume the internal shell wrapper. Use the web console for interactive work.

File transfer tools are intentionally conservative. MCP can list transfer
metadata, browse remote directories, start remote download queues, save
completed downloads to explicit local paths, upload explicit local files, and
pause/resume/cancel queues. `always_run` starts queues immediately.
`approval_required` creates a local approval queue in AIPermission Transfer
Center; the operator can approve selected files and reject the rest with a note.
MCP tool responses never include file contents, gateway temporary paths, archive
staging paths, or local upload contents.

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

These instructions teach the agent how to poll `approval_pending` and `running` requests, handle `stale` approvals by sending a fresh request, read live console output, recover stuck persistent console sessions with `restart_console_session`, write short reasons, use explicit file transfer paths, and avoid printing secrets. The default installer uses the operator instruction bundled in the npm package; `--source` accepts local file paths only and rejects HTTP(S) sources.

## Security Boundary

This package talks to a local AIPermission gateway. `AIPERMISSION_API_URL` must point to `localhost`, `127.0.0.1`, or `[::1]`; remote URLs are rejected before the bearer token is sent. Do not expose the gateway on LAN or the public internet, and do not use it as a shared DevOps service. Tokens grant access only to the servers and execution rules configured in the gateway UI. Token/server permissions may be temporary; expired grants are omitted from `list_servers` and no longer authorize command, console, or file-transfer tools. `list_servers` is permission-scoped, not a live SSH health check; treat `exec` connection errors as the current reachability signal.

## License

MIT
