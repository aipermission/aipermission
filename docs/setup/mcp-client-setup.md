# MCP Client Setup

This page explains how Cursor, Windsurf, Codex, Claude Code, VS Code, Gemini CLI, Antigravity, or another MCP client connects to the local aipermission gateway.

Prerequisites:

1. The gateway is running with `docker compose up`.
2. An API token exists in the web UI.
3. The token has at least one server permission.

For Docker runtime details, see [Docker Runtime](docker-runtime.md).

Project and package targets:

- GitHub: https://github.com/aipermission/aipermission
- npm package: https://www.npmjs.com/package/@aipermission/mcp

`@aipermission/mcp` is the official MCP bridge package. The unscoped `aipermission` npm package is a small placeholder that redirects users to the scoped package.

## Recommended Setup

Use the init command:

```bash
npx -y @aipermission/mcp init
```

`npx` downloads the package from npm and runs the init command. A global `npm install` is not required for normal use.

The command asks for:

1. AI client/provider
2. MCP server name
3. API token

Provider selection is interactive. Supported targets:

- OpenAI Codex
- Claude Code
- Cursor
- VS Code
- Windsurf
- Google Antigravity
- Gemini CLI
- Custom / copy-paste

If Custom is selected, the CLI does not write files and prints a JSON snippet instead.

Non-interactive use:

```bash
npx -y @aipermission/mcp init \
  --provider codex \
  --name cursor-maintenance
```

This form asks for the token through a hidden prompt. Prefer this over passing tokens as shell arguments.

The generated MCP config contains a bearer token. Keep it private. For project-local config files such as `.mcp.json`, `.cursor/mcp.json`, and `.vscode/mcp.json`, the init command refuses to write into files already tracked by Git unless `--force` is passed. For untracked project-local configs, it adds the file to `.git/info/exclude` when it detects a Git repository. This protects the local checkout without changing the project's shared `.gitignore`. If a token config is committed or shared, revoke that token in the web UI.

Full automation can pass the token through stdin:

```bash
printf '%s' "$AIPERMISSION_API_TOKEN" | npx -y @aipermission/mcp init \
  --provider codex \
  --name cursor-maintenance \
  --token-stdin
```

The default Docker gateway URL for MCP clients is `http://localhost:3210`. The frontend proxies `/api` to the backend. AIPermission is local-only; LAN/public gateway URLs are unsupported. For custom config, still use localhost:

```bash
npx -y @aipermission/mcp init \
  --provider custom \
  --name aipermission-local \
  --api-url http://localhost:3210 \
  --print
```

`--token TOKEN` remains for compatibility, but it is not recommended because shell history can store it.

## Manual Config

MCP server config shape:

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

If the related server permission is not `always_run`, a smoke test returns `approval_pending`. After the user clicks Run or Decline in Console, the MCP client checks the result with `get_request(request_id)`. Long-running `always_run` commands can be observed with `read_console(server_id)`.

Provider config file targets:

```txt
OpenAI Codex -> ~/.codex/config.toml
Claude Code  -> .mcp.json in the current project
Cursor       -> .cursor/mcp.json
VS Code      -> .vscode/mcp.json
Windsurf     -> ~/.codeium/windsurf/mcp_config.json
Antigravity  -> ~/.gemini/antigravity/mcp_config.json
Gemini CLI   -> ~/.gemini/settings.json
Custom       -> stdout only
```

Claude Code also supports its official `claude mcp add` / `claude mcp add-json` flow. The aipermission init command writes the same project-scoped `.mcp.json` shape so the configuration can be reviewed and committed or kept local according to the user's project policy.

Each token should be added as a separate MCP server name. For example, `cursor-maintenance`, `codex-readonly`, and `security-check` can use different tokens against the same local gateway.

## Local Package Development

Contributors who develop the MCP package can work from the monorepo package directory:

```bash
cd packages/mcp
npm install
npm test
npm run build
```

If an AI client is launched from the AIPermission monorepo root, `npx -y
@aipermission/mcp` can resolve the local workspace package instead of the
published npm package. In that development-only case, configure the MCP server
with the workspace command:

```json
{
  "command": "npm",
  "args": [
    "exec",
    "--yes",
    "--workspace",
    "packages/mcp",
    "--",
    "aipermission-mcp"
  ],
  "env": {
    "AIPERMISSION_API_URL": "http://localhost:3210",
    "AIPERMISSION_API_TOKEN": "TOKEN"
  }
}
```

For normal user projects, keep the standard `npx -y @aipermission/mcp` setup.

## Operator Instructions

Use [aipermission Operator Skill](../skills/aipermission-operator/SKILL.md) to standardize AI behavior around `approval_pending`, `running`, console polling, reasons, and secret hygiene.

Codex installs it as a native skill. Other clients receive the same content in their official persistent instruction/rule format. The CLI uses the operator instruction bundled inside the npm package by default. `--source` accepts local file paths only; HTTP(S) sources are rejected so remote content cannot silently rewrite AI instruction files.

### 1. Recommended: CLI Install

Run the command that matches your AI client:

```bash
npx -y @aipermission/mcp install-skill --client codex
npx -y @aipermission/mcp install-skill --client claude-code
npx -y @aipermission/mcp install-skill --client cursor
npx -y @aipermission/mcp install-skill --client vscode
npx -y @aipermission/mcp install-skill --client windsurf
npx -y @aipermission/mcp install-skill --client antigravity
npx -y @aipermission/mcp install-skill --client gemini
```

CLI targets:

```txt
Codex       -> ~/.codex/skills/aipermission-operator/SKILL.md
Claude Code -> .claude/rules/aipermission-operator.md
Cursor      -> .cursor/rules/aipermission-operator.mdc
VS Code     -> .github/instructions/aipermission-operator.instructions.md
Windsurf    -> .windsurf/rules/aipermission-operator.md
Antigravity -> .agents/rules/aipermission-operator.md
Gemini CLI  -> managed aipermission block inside GEMINI.md
```

Restart or open a new session in the AI client after installation.

The MCP bridge only accepts local gateway URLs: `http://localhost:3210`, `http://127.0.0.1:3210`, or `http://[::1]:3210`. It refuses remote `AIPERMISSION_API_URL` values so a poisoned config cannot send the bearer token to another host.

### 2. Manual Install / Custom

Manual Codex install:

```bash
mkdir -p ~/.codex/skills/aipermission-operator
curl -fsSL https://raw.githubusercontent.com/aipermission/aipermission/main/docs/skills/aipermission-operator/SKILL.md \
  -o ~/.codex/skills/aipermission-operator/SKILL.md
```

If docs have not yet been merged to `main`, use `dev`:

```bash
curl -fsSL https://raw.githubusercontent.com/aipermission/aipermission/dev/docs/skills/aipermission-operator/SKILL.md \
  -o ~/.codex/skills/aipermission-operator/SKILL.md
```

For custom clients, print portable Markdown:

```bash
npx -y @aipermission/mcp install-skill --client custom
```

### 3. Prompt-Level Use

If a client cannot load instruction files automatically, paste the generated instruction text into that client's custom instructions field.

## Expected Tools

The MCP client should see:

```txt
list_servers()
exec(server_id, command, reason?)
read_console(server_id, tail?)
restart_console_session(server_id)
get_request(request_id)
list_requests(status?)
send_message(message, server_id?, session_id?)
```

For tool details, see [MCP Tools](../api/mcp-tools.md).
