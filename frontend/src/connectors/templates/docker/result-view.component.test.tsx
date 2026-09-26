import { render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { DockerResourceDetail, DockerResultView } from "./result-view";
import type { DockerResourceKind } from "./resource-types";

const searchProps = { search: "", onSearch: vi.fn(), inputClass: "" };

it("preserves Docker inspect state, networking, zero values, and full raw data", () => {
  const output = {
    container: { id: "api-id", name: "api", image: "api:v1", state: "running", status: "Up 1 hour" },
    inspect: [
      {
        Name: "/api",
        Image: "sha256:abc",
        Created: "created",
        RestartCount: 0,
        State: {
          Status: "running",
          Running: true,
          Restarting: false,
          StartedAt: "started",
          FinishedAt: "finished",
          ExitCode: 0,
          Health: { Status: "healthy" },
        },
        Config: {
          Image: "api:v1",
          Entrypoint: ["/entrypoint"],
          Cmd: ["serve"],
          WorkingDir: "/app",
          User: "1000",
          Labels: { project: "demo" },
        },
        HostConfig: { NetworkMode: "bridge" },
        NetworkSettings: {
          Ports: { "80/tcp": [{ HostIp: "127.0.0.1", HostPort: "8080" }] },
          Networks: { apps: { IPAddress: "172.20.0.2" } },
        },
        Mounts: [{ Type: "volume", Source: "data", Destination: "/data" }],
        extension: { retained: true },
      },
    ],
  };
  render(<DockerResultView {...searchProps} item={{ action_name: "inspect_container", output }} />);
  expect(screen.getByText("Docker inspect metadata")).toBeVisible();
  expect(screen.getByText("127.0.0.1:8080->80/tcp")).toBeVisible();
  expect(screen.getByText("apps 172.20.0.2")).toBeVisible();
  expect(screen.getByText("volume data -> /data")).toBeVisible();
  expect(screen.getAllByText("0")).toHaveLength(2);
  expect(screen.getByText("1 labels")).toBeVisible();
  expect(screen.getByText("Docker inspect raw data")).toBeVisible();
  expect(screen.getByText(/"retained": true/)).toBeVisible();
  expect(screen.getByPlaceholderText("Search raw data")).toBeVisible();
});

it("renders untrusted logs as text and preserves raw scalar results", () => {
  const view = render(
    <DockerResultView
      {...searchProps}
      item={{ action_name: "container_logs", output: { container: { name: "api" }, tail: 50, logs: "<script>untrusted</script>" } }}
    />,
  );
  expect(screen.getByText("Container logs")).toBeVisible();
  expect(screen.getByText("api · tail 50")).toBeVisible();
  expect(screen.getByText("<script>untrusted</script>")).toBeVisible();
  expect(document.querySelector("script")).toBeNull();
  view.rerender(
    <DockerResultView {...searchProps} item={{ action_name: "docker_action", output: "raw text", display_text: "finished" }} />,
  );
  expect(screen.getByText('"raw text"')).toBeVisible();
  expect(screen.getByText("finished")).toBeVisible();
});

it.each<[DockerResourceKind, string]>([
  ["images", "image metadata"],
  ["networks", "network metadata"],
  ["volumes", "volume metadata"],
])("preserves %s detail projection and raw data", (resourceView, title) => {
  render(
    <DockerResourceDetail
      {...searchProps}
      resourceView={resourceView}
      item={{
        id: "resource-id",
        name: "resource",
        repository: "api",
        tag: "v1",
        digest: "sha256:abc",
        created_since: "1d",
        driver: "local",
        scope: "local",
        ipv6: "false",
        internal: "false",
        mountpoint: "/data",
        containers: 0,
      }}
    />,
  );
  expect(screen.getByText(title)).toBeVisible();
  expect(screen.getByText("0")).toBeVisible();
  expect(screen.getByPlaceholderText("Search raw data")).toBeVisible();
});
