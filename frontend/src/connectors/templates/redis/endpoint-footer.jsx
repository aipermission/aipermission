import { ConnectorEndpointFooter } from "../_shared/endpoint-footer";
import { serverProductLabel } from "./model";

export function RedisEndpointFooter({ target, borderClass, mutedClass }) {
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
