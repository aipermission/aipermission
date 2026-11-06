import { useId } from "react";
import { Field, FieldWithAction, Input, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { ConnectionModeFields } from "../_shared/network-transport-fields";
import { HostPingButton } from "../host-ping-button";
import { KafkaSASLFields } from "./sasl-fields";
import type { ConnectorFormProps } from "../_shared/connector-form-types";
import type { KafkaConnectionForm } from "./form-types";

export function KafkaConnectorFormTemplate({ form, mode = "create", targets = [], onChange }: ConnectorFormProps<KafkaConnectionForm>) {
  const editing = mode === "edit";
  const brokersID = useId();
  const saslException = form.sasl_mechanism === "plain" ? "allow_insecure_plain_sasl" : "allow_insecure_scram_sasl";
  const saslLabel = form.sasl_mechanism === "plain" ? "PLAIN" : "SCRAM";
  const authenticated = ["plain", "scram_sha_256", "scram_sha_512"].includes(form.sasl_mechanism);
  return (
    <>
      <Notice tone="good">
        Kafka and Redpanda share one connector. Start with read actions in Prompt mode; message samples can contain sensitive application
        data.
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
      <ConnectionModeFields
        form={form}
        targets={targets}
        onChange={onChange}
        transportNotice="Every broker address advertised by the cluster is reached through the selected SSH transport. Broker hostnames must resolve on that remote host."
        directNotice="The gateway must reach bootstrap and advertised broker addresses. For a broker on the Docker host, use host.docker.internal instead of localhost."
      />
      <FieldWithAction
        htmlFor={brokersID}
        label="Bootstrap brokers"
        action={
          <HostPingButton
            host={firstBrokerHost(form.bootstrap_brokers)}
            port={firstBrokerPort(form.bootstrap_brokers)}
            mode={form.connection_mode}
            transportTargetRef={form.transport_target_ref}
            projectID={Number(form.project_id) || 0}
          />
        }
      >
        <Textarea
          id={brokersID}
          className="min-h-24 font-mono text-xs"
          value={form.bootstrap_brokers}
          onChange={(event) => onChange("bootstrap_brokers", event.target.value)}
          placeholder={"broker-1:9092\nbroker-2:9092"}
          required
        />
        <span className="text-xs text-stone-500">One host:port per line, or a comma-separated list.</span>
      </FieldWithAction>
      <Field>
        Additional permitted brokers
        <Textarea
          className="min-h-24 font-mono text-xs"
          value={form.advertised_brokers || ""}
          onChange={(event) => onChange("advertised_brokers", event.target.value)}
          placeholder={"advertised-broker-1:9092\nadvertised-broker-2:9092"}
          title="Only bootstrap endpoints and these additional advertised endpoints may be contacted, in Direct or Over SSH mode. Maximum 200."
        />
      </Field>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field>
          TLS
          <Select
            value={form.tls_enabled ? "enabled" : "disabled"}
            onChange={(event) => onChange("tls_enabled", event.target.value === "enabled")}
          >
            <option value="disabled">Disabled</option>
            <option value="enabled">Enabled</option>
          </Select>
        </Field>
        <Field>
          TLS server name
          <Input
            value={form.tls_server_name}
            onChange={(event) => onChange("tls_server_name", event.target.value)}
            disabled={!form.tls_enabled}
            placeholder="Optional certificate hostname override"
          />
        </Field>
      </div>
      {!form.tls_enabled && authenticated ? (
        <Field>
          {saslLabel} without TLS
          <input
            type="checkbox"
            checked={Boolean(form[saslException])}
            onChange={(event) => onChange(saslException, event.target.checked)}
          />
          <span className="text-xs text-amber-600">
            {saslLabel === "PLAIN"
              ? "PLAIN sends credentials without encryption."
              : "SCRAM does not encrypt broker traffic or authentication exchanges."}{" "}
            Prefer TLS; SSH does not encrypt the final broker hop.
          </span>
        </Field>
      ) : null}
      {form.tls_enabled ? (
        <Field>
          Custom CA certificate
          <Textarea
            className="min-h-24 font-mono text-xs"
            value={form.tls_ca_pem}
            onChange={(event) => onChange("tls_ca_pem", event.target.value)}
            placeholder="Optional PEM certificate chain"
          />
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

function firstBrokerHost(value: string): string {
  const broker = firstBroker(value);
  const separator = broker.lastIndexOf(":");
  return separator > 0 ? broker.slice(0, separator).replace(/^\[|\]$/g, "") : broker;
}

function firstBrokerPort(value: string): number {
  const broker = firstBroker(value);
  const separator = broker.lastIndexOf(":");
  return separator > 0 ? Number(broker.slice(separator + 1)) || 9092 : 9092;
}

function firstBroker(value: string): string {
  return (
    String(value || "")
      .split(/[\s,]+/)
      .find(Boolean) || "127.0.0.1:9092"
  );
}
