import { expect, expectTypeOf, it } from "vitest";
import type { ComponentProps } from "react";
import type { ConnectorFieldChange } from "./connector-form-types";
import type { SSHConnectorFormTemplate } from "../ssh/form";
import type { SSHForm } from "../ssh/form-types";
import type { S3ConnectorFormTemplate } from "../s3/form";
import type { S3Form } from "../s3/model";
import { emptyForm } from "../s3/model";
import { NetworkTransportFields } from "./network-transport-fields";
import { fireEvent, render, screen } from "@testing-library/react";

it("correlates field names with their native values instead of accepting independent unions", () => {
  type Change = Parameters<ConnectorFieldChange<{ enabled: boolean; port: number | string; optional?: string }>>;
  expectTypeOf<Change>().toEqualTypeOf<["enabled", boolean] | ["port", number | string] | ["optional", string | undefined]>();
  expectTypeOf<["enabled", string]>().not.toExtend<Change>();
  expectTypeOf<["missing", boolean]>().not.toExtend<Change>();
  expectTypeOf<ComponentProps<typeof SSHConnectorFormTemplate>["onChange"]>().toEqualTypeOf<ConnectorFieldChange<SSHForm>>();
  expectTypeOf<ComponentProps<typeof S3ConnectorFormTemplate>["onChange"]>().toEqualTypeOf<ConnectorFieldChange<S3Form>>();
  expectTypeOf<ConnectorFieldChange<S3Form>>().toExtend<ComponentProps<typeof NetworkTransportFields>["onChange"]>();
  expectTypeOf<["project_id", number]>().not.toExtend<Parameters<ComponentProps<typeof NetworkTransportFields>["onChange"]>>();
});

it("binds a native S3 callback to network fields without making the project context writable", () => {
  const received: Parameters<ConnectorFieldChange<S3Form>>[] = [];
  const onChange: ConnectorFieldChange<S3Form> = (...update) => received.push(update);
  render(<NetworkTransportFields form={{ ...emptyForm(), project_id: 7 }} onChange={onChange} />);
  const host = screen.getByRole("textbox", { name: "Host" });
  expect(screen.getByLabelText("Host")).toBe(host);
  expect(host).not.toBe(screen.getByRole("button", { name: "Ping host" }));
  fireEvent.change(host, { target: { value: "objects.example.test" } });
  fireEvent.change(screen.getByRole("spinbutton", { name: "Port" }), { target: { value: "9000" } });
  expect(received).toEqual([
    ["host", "objects.example.test"],
    ["port", "9000"],
  ]);
});

it("keeps tuple correlation when an update is forwarded to a native controller", () => {
  const received: Parameters<ConnectorFieldChange<{ enabled: boolean; port: string | number }>>[] = [];
  const update: ConnectorFieldChange<{ enabled: boolean; port: string | number }> = (...change) => received.push(change);
  update("enabled", true);
  update("port", "443");
  update("port", 443);
  expect(received).toEqual([
    ["enabled", true],
    ["port", "443"],
    ["port", 443],
  ]);
});
