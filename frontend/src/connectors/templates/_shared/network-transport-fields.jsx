import { Field, Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";
import { connectorTemplateMetadata, getConnectorMetadata } from "../catalog";
import { publicEndpointValue, uniqueNetworkTransportDescriptors } from "./network-transport-contract";

export function NetworkTransportFields({
  form,
  targets = [],
  onChange,
  hostLabel = "Host",
  portLabel = "Port",
  transportNotice,
  directNotice,
}) {
  return (
    <>
      <ConnectionModeFields
        form={form}
        targets={targets}
        onChange={onChange}
        transportNotice={transportNotice}
        directNotice={directNotice}
      />
      <NetworkEndpointFields form={form} onChange={onChange} hostLabel={hostLabel} portLabel={portLabel} />
    </>
  );
}

export function ConnectionModeFields({ form, targets = [], onChange, transportNotice, directNotice }) {
  const usesTransport = form.connection_mode !== "direct";
  const transport = networkTransportDescriptors().find((item) => item.mode === form.connection_mode);
  return (
    <>
      <Field>
        Connection mode
        <Select value={form.connection_mode} onChange={(event) => onChange("connection_mode", event.target.value)}>
          <option value="direct">Direct from this gateway</option>
          {networkTransportDescriptors().map((transport) => (
            <option value={transport.mode} key={transport.mode}>
              {transport.option_label}
            </option>
          ))}
        </Select>
      </Field>
      {usesTransport ? (
        <TransportProfileField
          value={form.transport_target_ref}
          transportMode={form.connection_mode}
          label={transport?.profile_label}
          targets={targets}
          onChange={(value) => onChange("transport_target_ref", value)}
        />
      ) : null}
      {usesTransport && transportNotice ? <Notice>{transportNotice}</Notice> : null}
      {!usesTransport && directNotice ? <Notice>{directNotice}</Notice> : null}
    </>
  );
}

export function TransportProfileField({ value, transportMode, label = "Transport profile", targets = [], onChange }) {
  return (
    <Field>
      {label}
      <Select value={value} onChange={(event) => onChange(event.target.value)} required>
        <option value="" disabled>
          Select {label.toLowerCase()}
        </option>
        {transportProfileOptions(targets, transportMode).map((profile) => (
          <option value={profile.ref} key={profile.ref}>
            {profile.label}
          </option>
        ))}
      </Select>
    </Field>
  );
}

export function transportProfileOptions(targets, transportMode) {
  if (!transportMode) return [];
  return (targets || []).flatMap((target) => {
    const transport = getConnectorMetadata(target.connector_kind)?.network_transport;
    if (transport?.mode !== transportMode) return [];
    return (target.profiles || []).map((profile) => ({
      ref: profile.ref || `${target.connector_kind}:${target.id}:${profile.id}`,
      label: transportProfileOptionLabel(target, profile, transport),
    }));
  });
}

export function NetworkEndpointFields({
  form,
  onChange,
  hostLabel = "Host",
  portLabel = "Port",
  portPlaceholder,
  leading = null,
  trailing = null,
  className = "sm:grid-cols-[minmax(0,1fr)_120px]",
}) {
  return (
    <div className={`grid gap-3 ${className}`}>
      {leading}
      <Field>
        <span className="flex items-center justify-between gap-2">
          <span>{hostLabel}</span>
          <HostPingButton
            host={form.host}
            port={form.port}
            mode={form.connection_mode}
            transportTargetRef={form.transport_target_ref}
            projectID={form.project_id}
          />
        </span>
        <Input value={form.host} onChange={(event) => onChange("host", event.target.value)} required />
      </Field>
      <Field>
        {portLabel}
        <Input
          type="number"
          min="1"
          max="65535"
          value={form.port}
          onChange={(event) => onChange("port", event.target.value)}
          placeholder={portPlaceholder}
          required
        />
      </Field>
      {trailing}
    </div>
  );
}

function transportProfileOptionLabel(target, profile, transport) {
  const endpoint = (transport.profile_endpoint?.fields || [])
    .map((field) => publicEndpointValue({ target, profile }, field.path) ?? field.fallback)
    .filter((value) => value !== undefined && value !== null && value !== "")
    .join(transport.profile_endpoint?.separator || " ");
  return `${target.name} / ${profile.label}${endpoint ? ` · ${endpoint}` : ""}`;
}

export function networkTransportDescriptors() {
  return sortNetworkTransportDescriptors(uniqueNetworkTransportDescriptors(Object.entries(connectorTemplateMetadata)));
}

export function sortNetworkTransportDescriptors(descriptors) {
  return [...descriptors].sort((left, right) => left.option_label.localeCompare(right.option_label));
}
