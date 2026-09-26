import { Field, FieldWithAction, Input, Select } from "../../../components/ui/form";
import type { ReactNode } from "react";
import { useId } from "react";
import { Notice } from "../../../components/ui/notice";
import { HostPingButton } from "../host-ping-button";
import { connectorTemplateMetadata, getConnectorMetadata } from "../catalog";
import { publicEndpointValue, uniqueNetworkTransportDescriptors } from "./network-transport-contract";
import type { NetworkTransportDescriptor } from "./network-transport-contract";
import type { ConnectorFieldChange } from "./connector-form-types";

type TransportSelectionForm = {
  connection_mode: string;
  transport_target_ref: string;
};

type NetworkEndpointForm = {
  connection_mode: string;
  transport_target_ref?: string;
  host: string;
  port: string | number;
  project_id?: string | number;
};

type NetworkForm = TransportSelectionForm & NetworkEndpointForm;

export type NetworkTarget = {
  id: string | number;
  name: string;
  connector_kind: string;
  config?: Record<string, unknown>;
  profiles?: { id: string | number; ref?: string; label: string }[];
};

type NetworkFieldsProps = {
  form: NetworkForm;
  targets?: readonly NetworkTarget[];
  onChange: ConnectorFieldChange<Pick<NetworkForm, "connection_mode" | "transport_target_ref" | "host" | "port">>;
  hostLabel?: string;
  portLabel?: string;
  transportNotice?: string;
  directNotice?: string;
};

type EndpointFieldsProps = Pick<NetworkFieldsProps, "hostLabel" | "portLabel"> & {
  form: NetworkEndpointForm;
  onChange: ConnectorFieldChange<Pick<NetworkEndpointForm, "host" | "port">>;
  portPlaceholder?: string;
  leading?: ReactNode;
  trailing?: ReactNode;
  className?: string;
};

export function NetworkTransportFields({
  form,
  targets = [],
  onChange,
  hostLabel = "Host",
  portLabel = "Port",
  transportNotice,
  directNotice,
}: NetworkFieldsProps) {
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

export function ConnectionModeFields({
  form,
  targets = [],
  onChange,
  transportNotice,
  directNotice,
}: Pick<NetworkFieldsProps, "targets" | "transportNotice" | "directNotice"> & {
  form: TransportSelectionForm;
  onChange: ConnectorFieldChange<TransportSelectionForm>;
}) {
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

export function TransportProfileField({
  value,
  transportMode,
  label = "Transport profile",
  targets = [],
  onChange,
}: {
  value: string;
  transportMode: string;
  label?: string;
  targets?: readonly NetworkTarget[];
  onChange: (_value: string) => void;
}) {
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

export function TransportConnectorIdentityFields({
  form,
  targets = [],
  onChange,
}: Pick<NetworkFieldsProps, "targets"> & {
  form: TransportSelectionForm & { name: string };
  onChange: ConnectorFieldChange<Pick<TransportSelectionForm, "transport_target_ref"> & { name: string }>;
}) {
  return (
    <>
      <Field>
        Connector name
        <Input value={form.name} onChange={(event) => onChange("name", event.target.value)} required />
      </Field>
      <TransportProfileField
        value={form.transport_target_ref}
        transportMode={form.connection_mode}
        targets={targets}
        onChange={(value) => onChange("transport_target_ref", value)}
      />
    </>
  );
}

export function transportProfileOptions(targets: readonly NetworkTarget[], transportMode?: string) {
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
}: EndpointFieldsProps) {
  const hostID = useId();
  return (
    <div className={`grid gap-3 ${className}`}>
      {leading}
      <FieldWithAction
        htmlFor={hostID}
        label={hostLabel}
        action={
          <HostPingButton
            host={form.host}
            port={form.port}
            mode={form.connection_mode}
            transportTargetRef={form.transport_target_ref}
            projectID={Number(form.project_id) || 0}
          />
        }
      >
        <Input id={hostID} value={form.host} onChange={(event) => onChange("host", event.target.value)} required />
      </FieldWithAction>
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

function transportProfileOptionLabel(
  target: NetworkTarget,
  profile: NonNullable<NetworkTarget["profiles"]>[number],
  transport: Pick<NetworkTransportDescriptor, "profile_endpoint">,
) {
  const endpoint = (transport.profile_endpoint?.fields || [])
    .map((field) => publicEndpointValue({ target, profile }, field.path) ?? field.fallback)
    .filter((value) => value !== undefined && value !== null && value !== "")
    .join(transport.profile_endpoint?.separator || " ");
  return `${target.name} / ${profile.label}${endpoint ? ` · ${endpoint}` : ""}`;
}

export function networkTransportDescriptors() {
  return sortNetworkTransportDescriptors(uniqueNetworkTransportDescriptors(Object.entries(connectorTemplateMetadata)));
}

export function sortNetworkTransportDescriptors<Descriptor extends { option_label: string }>(
  descriptors: readonly Descriptor[],
): Descriptor[] {
  return [...descriptors].sort((left, right) => left.option_label.localeCompare(right.option_label));
}
