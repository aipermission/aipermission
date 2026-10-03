import { Component } from "react";
import type { ReactNode } from "react";
import { Notice } from "../../components/ui/notice";

type Props = { label: string; inputs: readonly unknown[]; children: ReactNode };
type State = { failed: boolean; inputs: Props["inputs"] };

export class InventoryRowBoundary extends Component<Props, State> {
  state: State = { failed: false, inputs: this.props.inputs };
  static getDerivedStateFromError(): Pick<State, "failed"> {
    return { failed: true };
  }
  static getDerivedStateFromProps(props: Props, state: State): State | null {
    return props.inputs.length === state.inputs.length && props.inputs.every((value, index) => value === state.inputs[index])
      ? null
      : { failed: false, inputs: props.inputs };
  }
  render() {
    if (!this.state.failed) return this.props.children;
    return (
      <tr>
        <td colSpan={6} className="px-4 py-4">
          <Notice tone="bad">{this.props.label}. Refresh the inventory and check the connector profile configuration.</Notice>
        </td>
      </tr>
    );
  }
}
