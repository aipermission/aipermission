import { Archive, UserPlus } from "lucide-react";
import { Button } from "../../../components/ui/button";

export function PostgresConnectorRowActionsTemplate({ onUnderConstruction }) {
  return (
    <>
      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Create user" onClick={() => onUnderConstruction("Create user")}>
        <UserPlus className="h-4 w-4" />
      </Button>
      <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Backup management" onClick={() => onUnderConstruction("Backup management")}>
        <Archive className="h-4 w-4" />
      </Button>
    </>
  );
}
