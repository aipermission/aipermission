import { optionalConsoleBoolean, optionalConsolePort, optionalConsoleText } from "../_shared/console-target-config";
import type { ConsolePresentationTarget } from "../_shared/console-presentation-types";

export type S3ConsoleTarget = {
  ref: string;
  name?: string;
  target_name?: string;
  transfer_runtime_id?: number | null;
  config?: {
    bucket?: string;
    scheme?: string;
    host?: string;
    port?: number | string;
    connection_mode?: string;
    transport_target_ref?: string;
    trust_conditional_requests?: boolean;
  };
};

export function s3ConsoleTarget(target: ConsolePresentationTarget & { transfer_runtime_id?: number | null }): S3ConsoleTarget {
  const config = target.config || {};
  return {
    ref: target.ref,
    name: target.name,
    target_name: target.target_name,
    transfer_runtime_id: target.transfer_runtime_id,
    config: {
      bucket: optionalConsoleText(config.bucket, "S3", "bucket"),
      scheme: optionalConsoleText(config.scheme, "S3", "scheme"),
      host: optionalConsoleText(config.host, "S3", "host"),
      port: optionalConsolePort(config.port, "S3"),
      connection_mode: optionalConsoleText(config.connection_mode, "S3", "connection_mode"),
      transport_target_ref: optionalConsoleText(config.transport_target_ref, "S3", "transport_target_ref"),
      trust_conditional_requests: optionalConsoleBoolean(config.trust_conditional_requests, "S3", "trust_conditional_requests"),
    },
  };
}
