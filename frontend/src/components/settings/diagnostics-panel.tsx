import { Download } from "lucide-react";
import { apiDownload } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Notice } from "../ui/notice";

export function DiagnosticsPanel() {
  const { actionState: state, runAction } = useAsyncAction();

  async function downloadDiagnostics() {
    if (state.state === "downloading") return;
    await runAction({
      pending: "downloading",
      action: async () => {
        try {
          return await apiDownload("/api/settings/diagnostics", `aipermission-diagnostics-${new Date().toISOString()}.json`);
        } catch (error) {
          throw new Error(errorMessage(error, "Could not download diagnostics."), { cause: error });
        }
      },
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Support diagnostics</CardTitle>
        <CardDescription>
          Download a bounded, redacted JSON report for troubleshooting local installation and runtime issues.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <Notice tone="neutral">
          The report excludes credentials, tokens, endpoints, target and database names, commands, payloads, message content, raw output,
          raw errors, and private paths.
        </Notice>
        <Button type="button" variant="outline" onClick={downloadDiagnostics} disabled={state.state === "downloading"}>
          <Download className="h-4 w-4" />
          {state.state === "downloading" ? "Preparing diagnostics..." : "Download diagnostics"}
        </Button>
        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      </CardContent>
    </Card>
  );
}
