import { Download } from "lucide-react";
import { useState } from "react";
import { apiDownload } from "../../lib/api";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Notice } from "../ui/notice";

export function DiagnosticsPanel() {
  const [state, setState] = useState({ status: "idle", error: "" });

  async function downloadDiagnostics() {
    setState({ status: "downloading", error: "" });
    try {
      await apiDownload("/api/settings/diagnostics", `aipermission-diagnostics-${new Date().toISOString()}.json`);
      setState({ status: "idle", error: "" });
    } catch (error) {
      setState({ status: "error", error: error.message || "Could not download diagnostics." });
    }
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
        <Button type="button" variant="outline" onClick={downloadDiagnostics} disabled={state.status === "downloading"}>
          <Download className="h-4 w-4" />
          {state.status === "downloading" ? "Preparing diagnostics..." : "Download diagnostics"}
        </Button>
        {state.status === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      </CardContent>
    </Card>
  );
}
