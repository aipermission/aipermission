# ADR 0001: Local-Only Gateway

Status: accepted

## Context

AIPermission lets AI agents run connector actions on developer-owned systems.
That capability should not be exposed as a shared web service without a much
larger security model.

## Decision

AIPermission is a localhost-only developer gateway.

The gateway runs on the developer's own machine. Remote systems are connector
targets, not places where the gateway is hosted for other users. Docker Compose
publishes only `127.0.0.1`, and the backend refuses non-loopback bind
addresses.

## Consequences

- LAN and public gateway hosting are unsupported.
- Host-header and origin checks are defense in depth, not remote security.
- Web REST uses a local browser session after unlock, not remote multi-user auth.
- Issues requesting remote/LAN hosting should usually be closed as `wontfix`.

## Related

- [Project Principles](../project-principles.md)
- [Local Gateway](../architecture/local-gateway.md)
- [Threat Model](../security/threat-model.md)
