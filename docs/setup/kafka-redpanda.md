# Kafka / Redpanda Connector

AIPermission uses one `kafka` connector kind for Apache Kafka and Redpanda.
Both products use the same target/profile/action permission, approval, history,
and audit pipeline.

## Connection

Create a connector and choose:

- **Server family:** Apache Kafka or Redpanda.
- **Connection mode:** Direct from the local gateway, or Over SSH through an
  existing SSH connector profile.
- **Bootstrap brokers:** one `host:port` per line or a comma-separated list.
- **TLS:** optional TLS, certificate server-name override, and custom CA PEM.
- **SASL profile:** None, PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512.

Kafka clients connect to the bootstrap broker first and then use broker
addresses advertised by cluster metadata. In Direct mode, every advertised
address must be reachable from the gateway container. In Over SSH mode, every
advertised address is dialed through the selected SSH profile and must resolve
from that remote host.

PLAIN SASL is rejected without TLS by default because it exposes the password
on the broker connection. A connector can explicitly allow insecure PLAIN only
for a trusted private network; TLS or SCRAM remains the recommended setup.

For a broker running on the same machine as AIPermission Docker, use
`host.docker.internal` rather than `localhost` in a Direct target.

## Actions

The read surface provides:

- `cluster_info`
- `list_topics`
- `describe_topic`
- `list_consumer_groups`
- `describe_consumer_group`
- `read_messages`

The 0.2.19 guarded write surface adds:

- `publish_message`
- `set_consumer_group_offset`

`read_messages` uses explicit topic/partition assignment. It does not join a
consumer group, commit offsets, or enable automatic topic creation. Record
count, returned serialized output, decompressed record batches, concurrent
fetches, and wait time are bounded. Large samples return a
`continuation_offset` instead of silently expanding the gateway response.

Message keys, values, and headers can contain secrets or customer data. Keep
message reads in Prompt mode unless the workflow is explicitly trusted. The
returned sample is stored in the encrypted local History like every other
connector action result.

`publish_message` writes one bounded key/value/header record to an explicit
partition. AIPermission requests all-in-sync-replica acknowledgements and does
not enable automatic topic creation. Delivery and retry time are bounded; if a
broker or network failure makes the outcome uncertain, inspect the topic before
retrying manually. Approval previews contain byte counts rather than raw
message content, and displayed request input redacts the raw key, value, and
headers.

`set_consumer_group_offset` changes one committed offset only. It checks that
the consumer group has no active members immediately before committing,
validates the requested offset against the partition's earliest and end
offsets, and reads the committed offset back after the write. Kafka does not
provide an atomic lock that prevents a member from joining between the final
check and commit. The result therefore reports this guard as best effort and
includes the post-commit group state or a warning when it cannot be confirmed.
Offset changes can replay already processed records or skip unread records, so
the action is classified destructive.
The current offset writer supports classic consumer groups. It fails closed for
groups using Kafka's newer consumer protocol rather than applying an offset
change through an ambiguous group API.

Keep both write actions in Prompt mode unless direct execution is deliberate
and tightly scoped. The generic MCP tools discover these actions through
`get_connector_actions` and execute them through `call_connector_action`; there
is no Kafka-specific MCP tool or bypass.

The local browser requires an explicit confirmation for each write and records
the result in shared History/Audit. Browser actions do not evaluate an MCP
token's Disabled/Prompt/Always rule; those rules govern MCP calls.

Use a least-privilege broker account. AIPermission bounds its own behavior, but
broker ACLs remain the primary authorization boundary.
