# Connectors

`internal/connectors` defines the small internal contract for connector-shaped
targets.

The package introduces shared vocabulary and types:

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

Built-in connector packages belong below this directory. Keep connector
implementations small: target schemas, credential schemas, help/action
metadata, preparation validation, and execution logic. Do not let a connector
write audit/history records directly.
