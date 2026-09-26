import { Component } from "react";
import type { ReactNode } from "react";
import type { InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { CredentialResource } from "../../lib/gateway-contracts/core-resource-contracts";
import { Notice } from "../../components/ui/notice";

type Props = { kind: string; targets: readonly InventoryTarget[]; credentials: readonly CredentialResource[]; children: ReactNode };
type State = { failed: boolean; targets: Props["targets"]; credentials: Props["credentials"] };

export class CredentialFamilyBoundary extends Component<Props, State> {
  state: State = { failed: false, targets: this.props.targets, credentials: this.props.credentials };
  static getDerivedStateFromError(): Pick<State, "failed"> {
    return { failed: true };
  }
  static getDerivedStateFromProps(props: Props, state: State): State | null {
    return props.targets === state.targets && props.credentials === state.credentials
      ? null
      : { failed: false, targets: props.targets, credentials: props.credentials };
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <tr>
        <td colSpan={6} className="px-4 py-4">
          <Notice tone="bad">
            Connector credentials unavailable: {this.props.kind}. Refresh the inventory and check the connector profile configuration.
          </Notice>
        </td>
      </tr>
    );
  }
}
