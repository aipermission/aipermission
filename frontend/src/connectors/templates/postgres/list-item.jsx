import { Archive, UserPlus } from "lucide-react";
import { Button } from "../../../components/ui/button";

export function PostgresConnectorRowActionsTemplate({ target, profile, onOperation }) {
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title="Create managed DB user"
        disabled={!profile}
        onClick={() => onOperation({ open: true, connector_kind: "postgres", type: "provision-user", target, profile, state: "idle", error: null })}
      >
        <UserPlus className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title="Backup / restore database"
        disabled={!profile}
        onClick={() => onOperation({ open: true, connector_kind: "postgres", type: "backup-restore", target, profile, state: "idle", error: null })}
      >
        <Archive className="h-4 w-4" />
      </Button>
    </>
  );
}
