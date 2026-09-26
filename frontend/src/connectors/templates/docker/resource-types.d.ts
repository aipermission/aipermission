export type DockerResourceKind = "containers" | "images" | "networks" | "volumes";

export interface DockerResource {
  id?: string;
  name?: string;
  repository?: string;
  tag?: string;
  image?: string;
  compose_project?: string;
  compose_service?: string;
  size?: string;
  driver?: string;
  scope?: string;
  mountpoint?: string;
  status?: string;
  health?: string;
  state?: string;
  created_since?: string;
  created_at?: string;
  containers?: number;
  labels?: string;
  ports?: string;
}
