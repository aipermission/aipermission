import { Archive, UserPlus } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { PostgresOperation } from "./operation-types";

export function PostgresConnectorRowActionsTemplate({
  target,
  profile,
  onOperation,
}: {
  target: NonNullable<PostgresOperation["target"]>;
  profile?: PostgresOperation["profile"] | null;
  onOperation: (_value: PostgresOperation) => void;
}) {
  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title="Create managed DB user"
        disabled={!profile}
        onClick={() =>
          profile &&
          onOperation({ open: true, connector_kind: "postgres", type: "provision-user", target, profile, state: "idle", error: null })
        }
      >
        <UserPlus className="h-4 w-4" />
      </Button>
      <Button
        type="button"
        variant="outline"
        className="h-9 w-9 px-0"
        title="Backup / restore database"
        disabled={!profile}
        onClick={() =>
          profile &&
          onOperation({ open: true, connector_kind: "postgres", type: "backup-restore", target, profile, state: "idle", error: null })
        }
      >
        <Archive className="h-4 w-4" />
      </Button>
    </>
  );
}
