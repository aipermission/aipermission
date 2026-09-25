import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { ConnectorEndpointFooter } from "./endpoint-footer";

it("renders connector-owned identity and endpoint content", () => {
  render(<ConnectorEndpointFooter leading="example:1:2" trailing={<strong>service.internal:443</strong>} />);
  expect(screen.getByText("example:1:2")).toBeInTheDocument();
  expect(screen.getByText("service.internal:443")).toBeInTheDocument();
});
