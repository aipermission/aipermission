# Connectors

`internal/connectors` defines the small internal contract for future
connector-shaped targets.

The package is intentionally inert in the current SSH runtime. It introduces
shared vocabulary and types only:

- connector
- target
- credential profile
- action definition
- prepared action
- action result
- registry

The gateway core remains responsible for token authentication, permission
rules, approval, redaction, history, and audit. Connectors describe available
actions and execute only after the gateway has allowed the action.

The initial design goal is:

```text
Connector knows the system.
Gateway knows permission.
AI knows only actions.
```

Do not add connector implementations here until the SSH behavior-preserving
adapter boundary is ready.
