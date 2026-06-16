# Use Cases

This note defines the target usage model for aipermission.

`aipermission` is not a DevOps platform. Its main use is helping a developer inspect, debug, and temporarily automate work on their own systems with AI assistance while keeping credentials out of the AI context.

Related notes:

- [What Is aipermission?](../whatis-aipermission.md)
- [MVP Scope](scope.md)
- [Local Gateway](../architecture/local-gateway.md)
- [MCP Permission Flow](../architecture/mcp-permission-flow.md)
- [Credential Boundary](../security/credential-boundary.md)

## Container Debug

When a container is failing on a VPS, the developer can give the AI temporary access to that server.

The AI can inspect:

- container list
- failing container logs
- recent restart timing
- volume, environment, and network settings
- exposed ports and listener state

Example:

```txt
The api container on core-1 is failing. Inspect logs, find the latest restart reason, and check config and network state.
```

This uses the SSH connector. SSH is one connector kind; the same permission,
approval, history, and audit pipeline is used for structured connectors such as
Postgres.

## Multi-Server Triage

When a system spans several VPS instances, the AI can inspect the allowed SSH
connector targets in one workflow.

Example:

```txt
Check service health on core-1, control-1, and worker-1. Determine whether the issue is application, network, or database related.
```

The gateway does not give credentials to the AI. It only allows connector
actions permitted by the token.

## Database And Structured Connector Inspection

When a developer needs database context, the AI can use a scoped Postgres
credential profile instead of receiving a connection string or password.

The AI can inspect:

- visible schemas
- visible tables
- column metadata
- bounded read-only query results

Example:

```txt
Use the readonly Postgres profile to describe the public billing tables and
summarize recent failed payment rows without changing data.
```

Future connector kinds such as Redis, queues, or API-backed integrations should
preserve the same model: connector target, credential profile, action,
permission, approval, history, and audit.

## Security Review

A developer may want a fast review for suspicious configuration or obvious exposure.

The AI can inspect:

- open ports
- unexpected processes
- new users
- SSH `authorized_keys` changes
- sudoers changes
- failed login attempts
- systemd services
- container image and port exposure
- disk, CPU, and network anomalies

This is an assisted review workflow. aipermission is not a SIEM, EDR, or vulnerability scanner.

## Suspicious Activity Triage

If compromise is suspected, the developer can grant access to specific servers for investigation.

Example:

```txt
I suspect this VPS may have suspicious activity. Check recent logins, new users, running processes, open ports, cron jobs, and container logs. Summarize findings and ask before deleting or changing anything risky.
```

For this scenario, `approval_required` is recommended. Read-only inspection can run, while destructive actions, restarts, firewall changes, or user/key changes should wait for explicit approval.

## Skill-Based Operations

Users can create their own AI client instructions or skills.

Example targets:

- add a new VPS to an existing private network
- verify Kubernetes node readiness
- validate internal DNS records
- test container runtime settings
- run service health checks

`aipermission` is the execution layer for those instructions. The skill describes what to do; the gateway controls where it runs, which token permits it, and whether approval is required.

## Guardrails

These boundaries do not change:

- AI does not see SSH private keys, database passwords, API credentials, or
  other connector secrets.
- AI sees only the connector targets and actions allowed by the token.
- Risky targets should use `approval_required`.
- Credential values must not be written into responses, audit logs, or MCP context.
- When the work is done, revoke the token or remove permissions.
