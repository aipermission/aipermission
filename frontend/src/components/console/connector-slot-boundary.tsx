import { Component } from "react";
import type { ReactNode } from "react";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";

type Props = { slot: "console" | "toolbar"; children: ReactNode; resetKey?: string };
type State = { failed: boolean; resetKey?: string };

export class ConnectorSlotBoundary extends Component<Props, State> {
  state: State = { failed: false, resetKey: this.props.resetKey };

  static getDerivedStateFromError(): Pick<State, "failed"> {
    return { failed: true };
  }

  static getDerivedStateFromProps(props: Props, state: State): State | null {
    return props.resetKey === state.resetKey ? null : { failed: false, resetKey: props.resetKey };
  }

  render() {
    if (!this.state.failed) return this.props.children;
    const { slot } = this.props;
    return (
      <div className={slot === "console" ? "grid h-full min-h-0 place-items-center p-4" : "flex min-w-0 items-center gap-2"}>
        <div className="grid min-w-0 gap-2">
          <Notice tone="bad">Connector {slot} unavailable.</Notice>
          <Button type="button" variant="outline" onClick={() => this.setState({ failed: false })}>
            Retry {slot}
          </Button>
        </div>
      </div>
    );
  }
}
