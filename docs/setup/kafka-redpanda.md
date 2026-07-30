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

## Read Actions

The 0.2.18 read surface provides:

- `cluster_info`
- `list_topics`
- `describe_topic`
- `list_consumer_groups`
- `describe_consumer_group`
- `read_messages`

`read_messages` uses explicit topic/partition assignment. It does not join a
consumer group, commit offsets, or enable automatic topic creation. Record
count, returned serialized output, decompressed record batches, concurrent
fetches, and wait time are bounded. Large samples return a
`continuation_offset` instead of silently expanding the gateway response.

Message keys, values, and headers can contain secrets or customer data. Keep
message reads in Prompt mode unless the workflow is explicitly trusted. The
returned sample is stored in the encrypted local History like every other
connector action result.

Use a least-privilege broker account. AIPermission bounds its own behavior, but
broker ACLs remain the primary authorization boundary.
