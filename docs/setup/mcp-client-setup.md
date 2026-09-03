# MCP Client Setup

This page explains how Codex, Claude Code, Cursor, VS Code, GitHub Copilot CLI,
Windsurf Cascade (legacy), Google Antigravity IDE or CLI, Gemini CLI, Grok Build CLI, or
another MCP client connects to the local AIPermission gateway.

Prerequisites:

1. The gateway is running with `docker compose up`.
2. An API token exists in the web UI.
3. The token has at least one connector target/profile/action permission.
4. Node.js 20+ and npm/npx are available in the AI client's child-process
   environment. GUI applications, WSL, and native Windows shells can have a
   different `PATH` from the terminal where setup is run.

For Docker runtime details, see [Docker Runtime](docker-runtime.md).

Project and package targets:

- GitHub: https://github.com/aipermission/aipermission
- npm package: https://www.npmjs.com/package/@aipermission/mcp

`@aipermission/mcp` is the official MCP bridge package. The unscoped `aipermission` npm package is a small placeholder that redirects users to the scoped package.

## Recommended Setup

Configure MCP and install the native operator skill in one command:

```bash
npx -y @aipermission/mcp setup
```

`npx` downloads the package from npm and runs the setup command. A global `npm install` is not required for normal use.

The command asks for:

1. AI client/provider
2. MCP server name
3. API token

Provider selection is interactive. Supported MCP targets:

- OpenAI Codex
- Claude Code
- Cursor
- VS Code
- GitHub Copilot CLI
- Windsurf Cascade (legacy)
- Google Antigravity IDE
- Google Antigravity CLI
- Gemini CLI
- Grok Build CLI
- Custom / copy-paste

If Custom is selected, the CLI does not write files and prints the MCP config
and canonical skill instead.

Non-interactive use:

```bash
npx -y @aipermission/mcp setup \
  --provider codex \
  --scope user \
  --name cursor-maintenance
```

This form asks for the token through a hidden prompt. Prefer this over passing tokens as shell arguments.

The generated MCP config contains a bearer token and pins the exact MCP package
version that wrote it. Keep it private. For project-local config files such as
`.mcp.json`, `.cursor/mcp.json`, and `.vscode/mcp.json`, setup refuses to write
into files already tracked by Git unless `--force` is passed. For untracked
project-local configs, it adds four paths to Git's local exclude file and
verifies them before writing: the final config, crash-safe temporary-file
pattern, Windows staging-directory pattern, and update lock. This protects the
local checkout without changing the shared `.gitignore`. Git inspection
failures stop setup rather than risking a token write.

Config updates use a cross-process lock, private POSIX modes or a restricted
Windows ACL, and atomic replacement so concurrent setup processes cannot
silently discard entries. A lock is never stolen automatically: after a crash,
first confirm no other setup process is running, then remove the
`.aipermission.lock` path named in the timeout error and retry. Config and skill
paths reject symbolic links or junctions anywhere in their managed path. Use
`init --print` and update a link-managed config through its owning tool. Print
mode never reads or prints a bearer token: it emits `YOUR_TOKEN_HERE`, writes no
config or skill, and requires you to replace the placeholder through the
client's private environment or config mechanism. Run `install-skill`
separately when needed.
If a token config is committed or shared, revoke that token in the web UI. Re-run setup
when you intentionally upgrade the package used by that client.

Setup validates the skill source and destination before asking for or writing
the token. It installs the skill first, then writes the sensitive config. A
config failure can therefore leave a harmless installed skill, but never a
token config followed by a failed skill installation.

Full automation can pass the token through stdin:

```bash
printf '%s' "$AIPERMISSION_API_TOKEN" | npx -y @aipermission/mcp setup \
  --provider codex \
  --scope user \
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

Do not pass tokens as shell arguments. Use the hidden prompt for interactive
setup or `--token-stdin` for automation. `--print` ignores token input and
produces only a placeholder preview.

Use `init` instead of `setup` when only the MCP config should be written. Use
`install-skill` when only the native skill should be installed.

Validate the selected config and skill without printing token values:

```bash
npx -y @aipermission/mcp doctor \
  --client codex \
  --scope user \
  --name aipermission
```

Doctor checks the expected client/scope paths, exact command arguments,
client-specific fields, pinned package version, local gateway URL, token
presence, private POSIX mode or Windows ACL, project Git protection, symlink
boundaries, and native skill metadata. It parses JSON and TOML without echoing
parser context and reports only status and paths, never the bearer token.
It is a static setup check: it does not launch the external AI client, resolve
that client's trust/precedence rules, or perform an MCP initialize handshake.

MCP and skill scopes can differ. For example, VS Code can keep workspace MCP
config while using the user skill directory:

```bash
npx -y @aipermission/mcp setup \
  --provider vscode \
  --scope project \
  --skill-scope user

npx -y @aipermission/mcp doctor \
  --client vscode \
  --mcp-scope project \
  --skill-scope user
```

## Manual Config

MCP server config shape:

```json
{
  "mcpServers": {
    "aipermission": {
      "command": "npx",
      "args": ["-y", "@aipermission/mcp@VERSION"],
      "env": {
        "NODE_ENV": "production",
        "AIPERMISSION_API_URL": "http://localhost:3210",
        "AIPERMISSION_API_TOKEN": "YOUR_TOKEN_HERE"
      }
    }
  }
}
```

Replace `VERSION` with the exact release you intend to pin. Setup writes the
current package version automatically. Copilot CLI output additionally includes
`"type": "local"` and `"tools": ["*"]`; Grok output sets
`startup_timeout_sec = 60` for cold `npx` startup.

If the related connector action permission is not `always_run`, a smoke test
returns `approval_pending`. After the user clicks Run or Decline in Console, the
MCP client checks the result with
`get_connector_action_request(request_id)`. Long-running SSH `exec` actions can
be observed with the SSH connector's `read_console` action. If an SSH request
remains running and the console shows no useful progress, the
`restart_console_session` connector action closes the gateway-owned persistent
console session so the next SSH `exec` action opens a fresh session.

Provider config file targets (`*` marks the default scope):

```txt
Client              User scope                              Project scope
OpenAI Codex        * ~/.codex/config.toml                  .codex/config.toml
Claude Code           ~/.claude.json                      * .mcp.json
Cursor                ~/.cursor/mcp.json                  * .cursor/mcp.json
VS Code                print for profile mcp.json        * .vscode/mcp.json
GitHub Copilot CLI  * ~/.copilot/mcp-config.json            .mcp.json
Windsurf Cascade (legacy) * ~/.codeium/windsurf/mcp_config.json unsupported
Antigravity IDE     * ~/.gemini/config/mcp_config.json       .agents/mcp_config.json
Antigravity CLI     * ~/.gemini/antigravity-cli/mcp_config.json .agents/mcp_config.json
Gemini CLI          * ~/.gemini/settings.json                .gemini/settings.json
Grok Build CLI      * ~/.grok/config.toml                    .grok/config.toml
Custom                stdout only                            stdout only
```

VS Code supports user-profile MCP configuration through **MCP: Open User
Configuration**, but the physical path depends on the active OS profile or
remote environment. AIPermission therefore prints the correct `servers` JSON
for user scope instead of guessing a path. Its project target remains automatic.

Client-specific home overrides are honored where documented: `CODEX_HOME` for
Codex MCP config, `CLAUDE_CONFIG_DIR` for Claude user skills, `COPILOT_HOME` for
Copilot config and skills, `GEMINI_CLI_HOME` for Gemini config and skills, and
`GROK_HOME` for Grok config and skills. Explicit `--home` is interpreted as a
home directory and takes precedence. Codex project config is loaded only after
the client trusts that project.

On native Windows, some clients cannot spawn `npx` directly even when it works
in an interactive terminal. First make Node.js 20+ visible to the GUI process
and restart that client. If it still reports `spawn npx ENOENT`, use `init --print`
and adapt the server command to `cmd` with arguments beginning
`["/d", "/s", "/c", "npx", "-y", "@aipermission/mcp@VERSION"]`.

Copilot CLI also accepts `.github/mcp.json`, but setup intentionally uses the
local `.mcp.json` project target because the generated object contains a bearer
token and must not become shared repository configuration.

Claude Code also supports its official `claude mcp add` / `claude mcp add-json`
flow. AIPermission writes the same project-scoped `.mcp.json` shape, but its
generated form contains a bearer token and must remain untracked/private. Use
`--print` and the client's own environment-variable mechanism if a shared
project config is required.

Each token should be added as a separate MCP server name. For example, `cursor-maintenance`, `codex-readonly`, and `security-check` can use different tokens against the same local gateway.

## Local Package Development

Contributors who develop the MCP package can work from the monorepo package directory:

```bash
cd packages/mcp
npm ci --workspaces=false
npm test
npm run build
```

If an AI client is launched from the AIPermission repository root, use the
built local package explicitly instead of allowing `npx -y @aipermission/mcp`
to resolve another package. In that development-only case, configure the MCP
server with the package entry point:

```json
{
  "command": "node",
  "args": ["packages/mcp/dist/cli.js"],
  "env": {
    "AIPERMISSION_API_URL": "http://localhost:3210",
    "AIPERMISSION_API_TOKEN": "TOKEN"
  }
}
```

For normal user projects, keep the standard `npx -y @aipermission/mcp` setup.

## Operator Instructions

Use [aipermission Operator Skill](../skills/aipermission-operator/SKILL.md) to standardize AI behavior around `approval_pending`, `running`, console polling, reasons, and secret hygiene.

Every supported client receives the same canonical `SKILL.md` in its native
skill directory. The CLI uses the operator skill bundled inside the npm package
by default. `--source` accepts local file paths only; HTTP(S) sources are
rejected so remote content cannot silently rewrite AI instruction files.
The MCP server also publishes a concise safety/workflow summary in its protocol
initialization response. Native skills remain the richer operating guide.

### 1. Recommended: CLI Install

Run the command that matches your AI client:

```bash
npx -y @aipermission/mcp install-skill --client codex
npx -y @aipermission/mcp install-skill --client claude-code
npx -y @aipermission/mcp install-skill --client cursor
npx -y @aipermission/mcp install-skill --client vscode
npx -y @aipermission/mcp install-skill --client copilot
npx -y @aipermission/mcp install-skill --client windsurf
npx -y @aipermission/mcp install-skill --client antigravity
npx -y @aipermission/mcp install-skill --client antigravity-cli
npx -y @aipermission/mcp install-skill --client gemini
npx -y @aipermission/mcp install-skill --client grok
npx -y @aipermission/mcp install-skill --client agents --scope project
```

Native skill targets (`aipermission-operator/SKILL.md` is appended to each
directory, and `*` marks the default scope):

```txt
Client              User scope                     Project scope
OpenAI Codex        * ~/.agents/skills              .agents/skills
Claude Code           ~/.claude/skills            * .claude/skills
Cursor                ~/.cursor/skills            * .cursor/skills
VS Code                ~/.copilot/skills          * .github/skills
GitHub Copilot CLI  * ~/.copilot/skills             .github/skills
Windsurf Cascade (legacy) * ~/.codeium/windsurf/skills .windsurf/skills
Antigravity IDE     * ~/.gemini/config/skills        .agents/skills
Antigravity CLI     * ~/.gemini/antigravity-cli/skills .agents/skills
Gemini CLI          * ~/.gemini/skills               .gemini/skills
Grok Build CLI      * ~/.grok/skills                 .grok/skills
Agents standard       ~/.agents/skills             * .agents/skills
```

Restart or open a new session in the AI client after installation. Some clients
also expose a narrower refresh action, such as Copilot CLI `/mcp reload`,
Gemini CLI `/skills reload`, Grok `/mcps`, or a VS Code MCP server restart. A
full restart remains the conservative cross-client recovery step.

The MCP bridge only accepts local gateway URLs: `http://localhost:3210`, `http://127.0.0.1:3210`, or `http://[::1]:3210`. It refuses remote `AIPERMISSION_API_URL` values so a poisoned config cannot send the bearer token to another host.

### 2. Manual Install / Custom

Manual Codex install:

```bash
mkdir -p ~/.agents/skills/aipermission-operator
curl -fsSL https://raw.githubusercontent.com/aipermission/aipermission/main/docs/skills/aipermission-operator/SKILL.md \
  -o ~/.agents/skills/aipermission-operator/SKILL.md
```

For custom clients, print portable Markdown:

```bash
npx -y @aipermission/mcp install-skill --client custom
```

### 3. Prompt-Level Use

If a client cannot load instruction files automatically, paste the generated instruction text into that client's custom instructions field.

## Unsupported Automatic Targets

Do not guess client config paths. Hosted Devin runs away from the developer
machine and cannot use this project's required `localhost` boundary, so it has
no AIPermission preset. Devin CLI and Devin Local do have documented local MCP
configuration: `~/.config/devin/mcp_config.json` for user scope,
`.devin/mcp_config.json` for project scope, and the untracked
`.devin/mcp_config.local.json` override. Windows uses the corresponding
`%APPDATA%\\devin\\mcp_config.json` user path. AIPermission does not auto-write
those files yet. Use `--provider custom --print` only when the Devin runtime
itself is local and can target the same local gateway; prefer the local override
for bearer-token config and keep it untracked.

## Expected Tools

The MCP client should see:

```txt
list_connector_targets()
get_connector_help(target_ref)
get_connector_actions(target_ref)
call_connector_action(target_ref, action_name, input?, reason?, idempotency_key)
get_connector_action_request(request_id)
list_vault_items(project_ref?)
call_vault_action(project_ref, action_name, input, reason, idempotency_key)
get_vault_action_request(request_id)
cancel_vault_action_request(request_id)
```

For tool details, see [MCP Tools](../api/mcp-tools.md).
