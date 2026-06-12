import { Container, Copy } from "lucide-react";
import { Button } from "../../../components/ui/button";

export function SSHConnectorRowActionsTemplate({ target, onOperation }) {
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title={target ? `Install key for ${target.name}` : "Install key"}
        disabled={!target}
        onClick={() => target && onOperation({ connector_kind: target.connector_kind, type: "install", target, open: true })}
      >
        <Copy className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title={target ? `Check Docker for ${target.name}` : "Check Docker"}
        disabled={!target}
        onClick={() => target && onOperation({ connector_kind: target.connector_kind, type: "docker-check", target, open: true, state: "idle" })}
      >
        <Container className="h-4 w-4" />
      </Button>
    </>
  );
}
