import { ConnectorEndpointFooter } from "../_shared/endpoint-footer";
import { serverProductLabel } from "./model";

type RedisTarget = {
  ref: string;
  config?: { host?: string; port?: number; database?: number; server_family?: string };
};

export function RedisEndpointFooter({
  target,
  borderClass,
  mutedClass,
}: {
  target: RedisTarget;
  borderClass?: string;
  mutedClass?: string;
}) {
  return (
    <ConnectorEndpointFooter
      leading={target.ref}
      trailing={`${serverProductLabel(target)} · ${target.config?.host}:${target.config?.port} db ${target.config?.database || 0}`}
      borderClass={borderClass}
      mutedClass={mutedClass}
      className="border-t px-3 py-2"
    />
  );
}
