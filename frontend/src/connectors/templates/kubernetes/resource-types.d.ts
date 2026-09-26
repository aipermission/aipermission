export type KubernetesResourceKind = "workloads" | "pods" | "services" | "ingress" | "nodes" | "events";

export interface KubernetesResource {
  namespace?: string;
  name?: string;
  kind?: string;
  object?: string;
  reason?: string;
  last_timestamp?: string;
  count?: number;
  message?: string;
  type?: string;
  node?: string;
  ready?: string;
  image?: string;
  cluster_ip?: string;
  ports?: string;
  hosts?: string;
  class?: string;
  roles?: string;
  version?: string;
  restarts?: number;
  phase?: string;
  age?: string;
  external_ip?: string;
}
