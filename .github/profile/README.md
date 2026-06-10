<div align="center">
  <img src="https://raw.githubusercontent.com/aipermission/aipermission/main/frontend/public/icon.svg" width="112" alt="AIPermission logo" />
  <h1>AIPermission</h1>
  <p><strong>Local permission gateway for AI agents.</strong></p>
  <p>
    Let AI assistants inspect and fix your own servers through scoped tokens,
    approval flows, live consoles, and audit history without giving the AI your
    SSH keys.
  </p>
  <p>
    <a href="https://github.com/aipermission/aipermission"><strong>Repository</strong></a>
    ·
    <a href="https://github.com/aipermission/aipermission/tree/main/docs"><strong>Docs</strong></a>
    ·
    <a href="https://github.com/aipermission/aipermission/blob/main/SECURITY.md"><strong>Security</strong></a>
    ·
    <a href="https://www.npmjs.com/package/@aipermission/mcp"><strong>MCP package</strong></a>
  </p>
  <p>
    <img alt="Local-only" src="https://img.shields.io/badge/security-local--only-064e3b" />
    <img alt="MCP" src="https://img.shields.io/badge/MCP-ready-0f766e" />
    <img alt="Runtime" src="https://img.shields.io/badge/runtime-Docker-2563eb" />
    <img alt="License" src="https://img.shields.io/badge/license-AGPL--3.0--only-111827" />
  </p>
</div>

---

## What We Are Building

AIPermission is for developers who already manage VPSes, containers, clusters,
or private services and want an AI assistant to help without receiving raw
credentials.

```txt
developer machine -> local AIPermission gateway -> SSH targets
AI client         -> MCP token                 -> scoped tools
```

The gateway owns credentials, permissions, approvals, persistent console
sessions, messages, history, and audit logs.

## Core Principles

| Principle | What it means |
| --- | --- |
| Local-only | The gateway runs on the developer's own machine and stays bound to localhost. |
| Credential boundary | SSH private keys and database passwords never leave the gateway. |
| Scoped tokens | Each AI/client token sees only the servers explicitly granted to it. |
| Human control | Commands can require Run / Decline approval before execution. |
| Observable work | Users can watch the same live console, send notes, and review history. |

## Start Here

- Main project: [github.com/aipermission/aipermission](https://github.com/aipermission/aipermission)
- MCP bridge: [`@aipermission/mcp`](https://www.npmjs.com/package/@aipermission/mcp)
- Security model: [SECURITY.md](https://github.com/aipermission/aipermission/blob/main/SECURITY.md)
- Roadmap: [docs/ROADMAP.md](https://github.com/aipermission/aipermission/blob/main/docs/ROADMAP.md)

## Not A Hosted DevOps Platform

AIPermission is pre-1.0 local developer software. It is not a LAN-shared
operations panel, hosted service, team RBAC product, or production DevOps
platform. Remote machines belong in the Servers list as SSH targets; they should
not host the gateway for other clients.

If you like the idea, try the RC, file issues, and help us make AI-assisted
server work safer and less tedious.
