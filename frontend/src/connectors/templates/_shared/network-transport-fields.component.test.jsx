import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConnectionModeFields, NetworkTransportFields } from "./network-transport-fields";

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

  it("keeps direct transport free of SSH profile controls", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ConnectionModeFields
        form={{ connection_mode: "direct", transport_target_ref: "" }}
        targets={targets}
        onChange={onChange}
        directNotice="Direct transport guidance"
        overSSHNotice="SSH transport guidance"
      />,
    );

    expect(screen.getByText("Direct transport guidance")).toBeVisible();
    expect(screen.queryByRole("combobox", { name: "SSH transport profile" })).not.toBeInTheDocument();
    await user.selectOptions(screen.getByRole("combobox", { name: "Connection mode" }), "over_ssh");
    expect(onChange).toHaveBeenCalledWith("connection_mode", "over_ssh");
  });

  it("lists only SSH connector profiles for over-SSH transport", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ConnectionModeFields
        form={{ connection_mode: "over_ssh", transport_target_ref: "" }}
        targets={targets}
        onChange={onChange}
        directNotice="Direct transport guidance"
        overSSHNotice="SSH transport guidance"
      />,
    );

    const profileSelect = screen.getByRole("combobox", { name: "SSH transport profile" });
    expect(screen.getByText("SSH transport guidance")).toBeVisible();
    expect(screen.getByRole("option", { name: "Operations / root · ops.example:2222" })).toBeVisible();
    expect(screen.queryByRole("option", { name: /Database/ })).not.toBeInTheDocument();
    await user.selectOptions(profileSelect, "ssh:4:8");
    expect(onChange).toHaveBeenCalledWith("transport_target_ref", "ssh:4:8");
  });
});
