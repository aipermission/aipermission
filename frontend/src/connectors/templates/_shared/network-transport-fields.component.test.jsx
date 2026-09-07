import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import {
  ConnectionModeFields,
  NetworkEndpointFields,
  networkTransportDescriptors,
  NetworkTransportFields,
  sortNetworkTransportDescriptors,
  transportProfileOptions,
} from "./network-transport-fields";

vi.mock("../host-ping-button", () => ({
  HostPingButton: (props) => <span data-testid="host-ping-props">{JSON.stringify(props)}</span>,
}));

const targets = [
  {
    connector_kind: "ssh",
    id: 4,
    name: "Operations",
    config: { host: "ops.example", port: 2222 },
    profiles: [{ id: 8, label: "root" }],
  },
  {
    connector_kind: "postgres",
    id: 5,
    name: "Database",
    profiles: [{ id: 9, label: "readonly" }],
  },
];

describe("ConnectionModeFields", () => {
  it("discovers transport modes and profile defaults from connector metadata", () => {
    expect(networkTransportDescriptors()).toEqual([
      expect.objectContaining({ mode: "over_ssh", option_label: "Over an SSH connector profile" }),
    ]);
    expect(
      transportProfileOptions(
        [{ connector_kind: "ssh", id: 4, name: "Operations", config: { host: "ops.example" }, profiles: [{ id: 8, label: "root" }] }],
        "over_ssh",
      ),
    ).toEqual([{ ref: "ssh:4:8", label: "Operations / root · ops.example:22" }]);
    expect(transportProfileOptions(targets, "unsupported_transport")).toEqual([]);
    expect(sortNetworkTransportDescriptors([{ option_label: "Zulu" }, { option_label: "Alpha" }])).toEqual([
      { option_label: "Alpha" },
      { option_label: "Zulu" },
    ]);
  });

  it("renders composable endpoint fields without owning connector-specific siblings", () => {
    const onChange = vi.fn();
    render(
      <NetworkEndpointFields
        form={{ connection_mode: "direct", host: "queue.example", port: "15672" }}
        onChange={onChange}
        hostLabel="Management host"
        leading={<span>Scheme field</span>}
        trailing={<span>Vhost field</span>}
      />,
    );

    expect(screen.getByText("Scheme field")).toBeVisible();
    expect(screen.getByText("Vhost field")).toBeVisible();
    fireEvent.change(screen.getByRole("spinbutton", { name: "Port" }), { target: { value: "15673" } });
    expect(onChange).toHaveBeenCalledWith("port", "15673");
  });

  it("wires host, port, mode, project, and transport profile through the shared fields", () => {
    const onChange = vi.fn();
    render(
      <NetworkTransportFields
        form={{
          connection_mode: "direct",
          transport_target_ref: "ssh:4:8",
          project_id: 7,
          host: "cache.example",
          port: "6379",
        }}
        targets={targets}
        onChange={onChange}
        hostLabel="Cache host"
        portLabel="Cache port"
      />,
    );

    const pingProps = JSON.parse(screen.getByTestId("host-ping-props").textContent);
    expect(pingProps).toEqual({
      host: "cache.example",
      port: "6379",
      mode: "direct",
      transportTargetRef: "ssh:4:8",
      projectID: 7,
    });
    const hostInput = screen.getByDisplayValue("cache.example");
    fireEvent.change(hostInput, { target: { value: "new-host" } });
    fireEvent.change(screen.getByRole("spinbutton", { name: "Cache port" }), { target: { value: "6380" } });
    expect(onChange).toHaveBeenCalledWith("host", "new-host");
    expect(onChange).toHaveBeenCalledWith("port", "6380");
  });

  it("keeps direct transport free of profile controls", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ConnectionModeFields
        form={{ connection_mode: "direct", transport_target_ref: "" }}
        targets={targets}
        onChange={onChange}
        directNotice="Direct transport guidance"
        transportNotice="SSH transport guidance"
      />,
    );

    expect(screen.getByText("Direct transport guidance")).toBeVisible();
    expect(screen.queryByRole("combobox", { name: "SSH connector profile" })).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole("combobox", { name: "Connection mode" }), "over_ssh");
    expect(onChange).toHaveBeenCalledWith("connection_mode", "over_ssh");
  });

  it("lists only connectors that provide network transport", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ConnectionModeFields
        form={{ connection_mode: "over_ssh", transport_target_ref: "" }}
        targets={targets}
        onChange={onChange}
        directNotice="Direct transport guidance"
        transportNotice="SSH transport guidance"
      />,
    );

    const profileSelect = screen.getByRole("combobox", { name: "SSH connector profile" });
    expect(screen.getByText("SSH transport guidance")).toBeVisible();
    expect(screen.getByRole("option", { name: "Operations / root · ops.example:2222" })).toBeVisible();
    expect(screen.queryByRole("option", { name: /Database/ })).not.toBeInTheDocument();
    await user.selectOptions(profileSelect, "ssh:4:8");
    expect(onChange).toHaveBeenCalledWith("transport_target_ref", "ssh:4:8");
  });
});
