import { Container, Copy, ShieldAlert } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { SSHTarget, SSHProfile } from "./form-types";

export function SSHConnectorRowActionsTemplate({
  target,
  profile,
  onOperation,
}: {
  target: SSHTarget | null;
  profile: SSHProfile | null;
  onOperation: (_operation: {
    connector_kind: string;
    type: "install" | "docker-check" | "key-cleanup";
    target: SSHTarget;
    profile?: SSHProfile;
    open: true;
    state?: "idle";
  }) => void;
}) {
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title={target ? `Install key for ${target.name}` : "Install key"}
        disabled={!target || !profile}
        onClick={() =>
          target && profile && onOperation({ connector_kind: target.connector_kind, type: "install", target, profile, open: true })
        }
      >
        <Copy className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title="Reconcile key cleanup"
        disabled={!target}
        onClick={() => target && onOperation({ connector_kind: target.connector_kind, type: "key-cleanup", target, open: true })}
      >
        <ShieldAlert className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title={target ? `Check Docker for ${target.name}` : "Check Docker"}
        disabled={!target || !profile}
        onClick={() =>
          target &&
          profile &&
          onOperation({ connector_kind: target.connector_kind, type: "docker-check", target, profile, open: true, state: "idle" })
        }
      >
        <Container className="h-4 w-4" />
      </Button>
    </>
  );
}
