# Built-In Connectors

<!-- Generated from frontend connector metadata. Do not edit directly. -->

All built-in connectors use the same project, target, credential profile,
token action permission, approval, history, and audit pipeline. Their
connector packages own protocol-specific validation and execution; frontend
templates own connector-specific forms and activity surfaces.

| Connector | Kind | Contract | Summary |
| --- | --- | --- | --- |
| ClickHouse | `clickhouse` | 0.1 | Metadata inspection and bounded read-only analytics SQL through ClickHouse credential profiles. |
| Docker | `docker` | 0.2 | Bounded container inventory, logs, inspect, and lifecycle actions through an SSH transport profile. |
| Kafka / Redpanda | `kafka` | 0.3 | Kafka-compatible metadata, bounded message inspection, guarded single-message publish, and inactive-group offset control through direct or SSH-transported profiles. |
| Kubernetes | `kubernetes` | 0.2 | Read-heavy Kubernetes cluster browser with bounded logs and prompt-only rollout restart. |
| Mail | `mail` | 0.2 | Bounded IMAP browsing, explicit read state changes, guarded mailbox moves, and SMTP send/reply actions. |
| Postgres | `postgres` | 0.2 | Schema inspection and bounded read-only SQL through database credential profiles. |
| RabbitMQ | `rabbitmq` | 0.2 | Queue browsing, bounded message previews, and explicit message publishing through RabbitMQ Management API profiles. |
| Redis / Valkey | `redis` | 0.2 | Redis-compatible key browsing, bounded reads, string writes, TTL updates, and destructive deletes through encrypted credential profiles. |
| S3 | `s3` | 0.3 | S3-compatible browsing, bounded transfer queues, temporary URLs, object versions, and lifecycle controls. |
| SSH | `ssh` | 0.2 | Persistent shell, file transfer, remote browsing, and command execution. |

Connector contract versions describe the in-project connector surface; they
are not package or AIPermission release versions.

For implementation rules, see [Add A Connector](development/add-a-connector.md).
For setup-specific guidance, use the [documentation index](index.md).
