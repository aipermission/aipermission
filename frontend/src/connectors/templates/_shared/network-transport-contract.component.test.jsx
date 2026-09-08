import { expect, it } from "vitest";

import { assertNetworkTransportMetadata, publicEndpointValue, uniqueNetworkTransportDescriptors } from "./network-transport-contract.js";

const transport = {
  mode: "over_example",
  label: "Example",
  option_label: "Over an example profile",
  profile_label: "Example profile",
  profile_endpoint: {
    fields: [
      { path: "target.config.host", fallback: "host" },
      { path: "profile.public.port", fallback: 443 },
    ],
    separator: ":",
  },
};

it("accepts typed transport metadata and reads only public endpoint paths", () => {
  expect(() => assertNetworkTransportMetadata("example", undefined)).not.toThrow();
  expect(() => assertNetworkTransportMetadata("example", transport)).not.toThrow();
  const value = {
    target: { name: "Example", config: { host: "example.test" } },
    profile: { label: "main", public: { port: 8443 }, secret: { token: "no" } },
  };
  expect(publicEndpointValue(value, "target.name")).toBe("Example");
  expect(publicEndpointValue(value, "target.config.host")).toBe("example.test");
  expect(publicEndpointValue(value, "profile.label")).toBe("main");
  expect(publicEndpointValue(value, "profile.public.port")).toBe(8443);
  expect(publicEndpointValue(value, "profile.secret.token")).toBeUndefined();
  expect(publicEndpointValue(value, "")).toBeUndefined();
});

it("rejects coercible metadata, unsafe paths, and missing fallbacks", () => {
  const invalid = [
    [],
    { ...transport, mode: { toString: () => "over_example" } },
    { ...transport, profile_endpoint: [] },
    { ...transport, profile_endpoint: { fields: [], separator: ":" } },
    { ...transport, profile_endpoint: { fields: transport.profile_endpoint.fields, separator: 1 } },
    {
      ...transport,
      profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: ["target", "config", "host"], fallback: "host" }] },
    },
    {
      ...transport,
      profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: "profile.secret.password", fallback: "hidden" }] },
    },
    { ...transport, profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: "target.__proto__.host", fallback: "host" }] } },
    { ...transport, profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: "target.config.host" }] } },
    { ...transport, profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: "target.config.host", fallback: "" }] } },
    { ...transport, profile_endpoint: { ...transport.profile_endpoint, fields: [{ path: "target.config.port", fallback: Infinity }] } },
  ];
  for (const candidate of invalid) expect(() => assertNetworkTransportMetadata("example", candidate)).toThrow();
});

it("rejects conflicting providers for one transport mode", () => {
  expect(() =>
    uniqueNetworkTransportDescriptors([
      ["first", { network_transport: transport }],
      ["second", { network_transport: { ...transport, label: "Other" } }],
    ]),
  ).toThrow(/conflicting network_transport mode/);
  expect(
    uniqueNetworkTransportDescriptors([
      ["empty", {}],
      ["first", { network_transport: transport }],
      [
        "second",
        {
          network_transport: {
            profile_label: transport.profile_label,
            option_label: transport.option_label,
            label: transport.label,
            mode: transport.mode,
            profile_endpoint: transport.profile_endpoint,
          },
        },
      ],
    ]),
  ).toEqual([transport]);
});
