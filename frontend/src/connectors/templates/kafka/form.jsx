import { Field, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { sshProfileOptions } from "../_shared/network-transport-fields";
import { HostPingButton } from "../host-ping-button";
import { KafkaSASLFields } from "./sasl-fields";

export function KafkaConnectorFormTemplate({ form, mode = "create", targets = [], onChange }) {
  const editing = mode === "edit";
  const overSSH = form.connection_mode === "over_ssh";
  const sshProfiles = sshProfileOptions(targets);
  return (
    <>
      <Notice tone="good">
        Kafka and Redpanda share one connector. Start with read actions in Prompt mode; message samples can contain sensitive application data.
      </Notice>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <Field>
        Server family
        <Select value={form.server_family} onChange={(event) => onChange("server_family", event.target.value)}>
          <option value="kafka">Apache Kafka</option>
          <option value="redpanda">Redpanda</option>
        </Select>
      </Field>
      <Field>
        Connection mode
        <Select value={form.connection_mode} onChange={(event) => onChange("connection_mode", event.target.value)}>
          <option value="direct">Direct from this gateway</option>
          <option value="over_ssh">Over an SSH connector profile</option>
        </Select>
      </Field>
      {overSSH ? (
        <Field>
          SSH transport profile
          <Select value={form.transport_target_ref} onChange={(event) => onChange("transport_target_ref", event.target.value)} required>
            <option value="" disabled>Select SSH profile</option>
            {sshProfiles.map((profile) => <option value={profile.ref} key={profile.ref}>{profile.label}</option>)}
          </Select>
        </Field>
      ) : null}
      <Notice>
        {overSSH
          ? "Every broker address advertised by the cluster is reached through the selected SSH transport. Broker hostnames must resolve on that remote host."
          : "The gateway must reach bootstrap and advertised broker addresses. For a broker on the Docker host, use host.docker.internal instead of localhost."}
      </Notice>
      <Field>
        <span className="flex items-center justify-between gap-2">
          <span>Bootstrap brokers</span>
          <HostPingButton
            host={firstBrokerHost(form.bootstrap_brokers)}
            port={firstBrokerPort(form.bootstrap_brokers)}
            mode={form.connection_mode}
            transportTargetRef={form.transport_target_ref}
          />
        </span>
        <Textarea
          className="min-h-24 font-mono text-xs"
          value={form.bootstrap_brokers}
          onChange={(event) => onChange("bootstrap_brokers", event.target.value)}
          placeholder={"broker-1:9092\nbroker-2:9092"}
          required
        />
        <span className="text-xs text-stone-500">One host:port per line, or a comma-separated list.</span>
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          TLS
          <Select value={form.tls_enabled ? "enabled" : "disabled"} onChange={(event) => onChange("tls_enabled", event.target.value === "enabled")}>
            <option value="disabled">Disabled</option>
            <option value="enabled">Enabled</option>
          </Select>
        </Field>
        <Field>
          TLS server name
          <Input value={form.tls_server_name} onChange={(event) => onChange("tls_server_name", event.target.value)} disabled={!form.tls_enabled} placeholder="Optional certificate hostname override" />
        </Field>
      </div>
      {!form.tls_enabled && form.sasl_mechanism === "plain" ? (
        <Field>
          PLAIN without TLS
          <Select
            value={form.allow_insecure_plain_sasl ? "allowed" : "blocked"}
            onChange={(event) => onChange("allow_insecure_plain_sasl", event.target.value === "allowed")}
          >
            <option value="blocked">Blocked</option>
            <option value="allowed">Allow on this trusted network</option>
          </Select>
          <span className="text-xs text-amber-600">PLAIN sends credentials without transport encryption. Prefer TLS or SCRAM.</span>
        </Field>
      ) : null}
      {form.tls_enabled ? (
        <Field>
          Custom CA certificate
          <Textarea className="min-h-24 font-mono text-xs" value={form.tls_ca_pem} onChange={(event) => onChange("tls_ca_pem", event.target.value)} placeholder="Optional PEM certificate chain" />
        </Field>
      ) : null}
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          Profile label
          <Input value={form.profile_label} onChange={(event) => onChange("profile_label", event.target.value)} required />
        </Field>
        <Field>
          Risk label
          <Input value={form.risk_label} onChange={(event) => onChange("risk_label", event.target.value)} />
        </Field>
      </div>
      <KafkaSASLFields form={form} editing={editing} onChange={onChange} />
    </>
  );
}

function firstBrokerHost(value) {
  const broker = firstBroker(value);
  const separator = broker.lastIndexOf(":");
  return separator > 0 ? broker.slice(0, separator).replace(/^\[|\]$/g, "") : broker;
}

function firstBrokerPort(value) {
  const broker = firstBroker(value);
  const separator = broker.lastIndexOf(":");
  return separator > 0 ? Number(broker.slice(separator + 1)) || 9092 : 9092;
}

function firstBroker(value) {
  return String(value || "").split(/[\s,]+/).find(Boolean) || "127.0.0.1:9092";
}
