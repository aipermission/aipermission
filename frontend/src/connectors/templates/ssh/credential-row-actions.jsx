import { CopyButton } from "../../../components/ui/copy-button";

export function SSHCredentialRowActionsTemplate({ row }) {
  const installCommand = row.credential?.install_command || "";
  if (!installCommand) return null;
  return (
    <CopyButton value={installCommand} variant="outline" className="h-9 w-9 px-0" title="Copy install command">
      {null}
    </CopyButton>
  );
}
