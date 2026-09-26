import { Database } from "lucide-react";
import { useEffect, useState, type Dispatch, type SetStateAction } from "react";
import { FileTransferDialog } from "../../../components/file-transfer/file-transfer-dialog";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { StructuredSessionEmpty } from "../_shared/structured-session-empty";
import { S3ConfirmDialog, S3UploadDialog } from "./dialogs";
import { S3EndpointFooter } from "./endpoint-footer";
import { S3LifecycleDialog } from "./lifecycle-dialog";
import { S3ObjectBrowser } from "./object-browser";
import { S3ObjectDetailPane } from "./object-detail-pane";
import { S3PresignDialog } from "./presign-dialog";
import { joinTransferPath, normalizeTransferDirectory } from "./transfer-paths";
import { useS3Browser, type S3BrowserOptions } from "./use-s3-browser";
import { useS3ObjectDelete } from "./use-s3-object-delete";
import { useS3Upload } from "./use-s3-upload";
import { S3VersionsDialog } from "./versions-dialog";

type S3ConsoleTarget = S3BrowserOptions["target"] & {
  name?: string;
  target_name?: string;
  transfer_runtime_id?: number | null;
  config?: {
    bucket?: string;
    scheme?: string;
    host?: string;
    port?: number | string;
    connection_mode?: string;
    transport_target_ref?: string;
    trust_conditional_requests?: boolean;
  };
};

type S3ConsoleProps = Omit<S3BrowserOptions, "target"> & {
  target: S3ConsoleTarget;
  theme: string;
  onNewStructuredSession: () => void;
};

export function S3ConnectorConsoleTemplate({
  target,
  approvals,
  theme,
  session,
  onNewStructuredSession,
  onRefreshActivity,
}: S3ConsoleProps) {
  const classes = connectorConsoleTheme(theme);
  const browser = useS3Browser({ target, approvals, session, onRefreshActivity });
  const scopeKey = JSON.stringify([target.ref, browser.activeSession.active, browser.activeSession.startedAt]);
  const [transferOpen, setTransferOpen] = useState(false);
  const [presignOpen, setPresignOpen] = useState(false);
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [lifecycleOpen, setLifecycleOpen] = useState(false);
  const upload = useS3Upload({
    scopeKey,
    active: browser.activeSession.active,
    prefix: browser.prefix,
    runAction: browser.runS3Action,
    refreshObjects: browser.refreshObjects,
    readObjectMetadata: browser.readObjectMetadata,
    setState: browser.setState,
  });
  const deletion = useS3ObjectDelete({
    scopeKey,
    selectedKey: browser.selectedKey,
    selectedETag: browser.metadata?.etag || browser.selectedObject?.etag,
    trustConditionalRequests: target.config?.trust_conditional_requests === true,
    runAction: browser.runS3Action,
    clearSelection: browser.clearSelection,
    refreshObjects: browser.refreshObjects,
  });

  useEffect(() => {
    setTransferOpen(false);
    setPresignOpen(false);
    setVersionsOpen(false);
    setLifecycleOpen(false);
  }, [scopeKey]);

  if (!browser.activeSession.active) {
    return (
      <StructuredSessionEmpty
        icon={Database}
        title="No active S3 session"
        description="Start a structured session to browse objects through the connector approval, history, and audit pipeline."
        buttonLabel="Start S3 session"
        onStart={onNewStructuredSession}
        panelClass={classes.panel}
        mutedClass={classes.muted}
        footer={<S3EndpointFooter target={target} borderClass={classes.border} mutedClass={classes.muted} />}
      />
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${classes.panel}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 xl:grid-cols-[380px_minmax(0,1fr)]">
        <S3ObjectBrowser
          target={target}
          directories={browser.directories}
          objects={browser.objects}
          prefix={browser.prefix}
          search={browser.search}
          selectedKey={browser.selectedKey}
          nextToken={browser.nextToken}
          latestAction={browser.latestAction}
          state={browser.state}
          classes={classes}
          onPrefixChange={browser.setPrefix}
          onSearchChange={browser.setSearch}
          onSearch={() => void browser.refreshObjects({ reset: true })}
          onBucketInfo={() => void browser.readBucketInfo()}
          onOpenTransfer={() => setTransferOpen(true)}
          onOpenUpload={upload.openUploadDialog}
          onRefresh={() => void browser.refreshObjects({ reset: true })}
          onOpenParent={() => void browser.openParentDirectory()}
          onOpenDirectory={(prefix) => void browser.openDirectory(prefix)}
          onSelectObject={(key) => void browser.selectObject(key)}
          onLoadMore={() => void browser.refreshObjects({ reset: false, token: browser.nextToken })}
        />
        <S3ObjectDetailPane
          active={browser.activeSession.active}
          selectedKey={browser.selectedKey}
          selectedObject={browser.selectedObject}
          metadata={browser.metadata}
          directories={browser.directories}
          objects={browser.objects}
          visibleBytes={browser.visibleBytes}
          prefix={browser.prefix}
          search={browser.search}
          metadataSearch={browser.metadataSearch}
          state={browser.state}
          classes={classes}
          onMetadataSearch={browser.setMetadataSearch}
          onOpenLifecycle={() => setLifecycleOpen(true)}
          onOpenPresign={() => setPresignOpen(true)}
          onOpenVersions={() => setVersionsOpen(true)}
          onDownload={() => void browser.downloadSelected()}
          onDelete={deletion.requestDelete}
        />
      </div>
      <S3EndpointFooter target={target} borderClass={classes.border} mutedClass={classes.muted} />
      <S3ConsoleDialogs
        scopeKey={scopeKey}
        target={target}
        theme={theme}
        classes={classes}
        browser={browser}
        upload={upload}
        deletion={deletion}
        transferOpen={transferOpen}
        setTransferOpen={setTransferOpen}
        presignOpen={presignOpen}
        setPresignOpen={setPresignOpen}
        versionsOpen={versionsOpen}
        setVersionsOpen={setVersionsOpen}
        lifecycleOpen={lifecycleOpen}
        setLifecycleOpen={setLifecycleOpen}
      />
    </div>
  );
}

function S3ConsoleDialogs({
  scopeKey,
  target,
  theme,
  classes,
  browser,
  upload,
  deletion,
  transferOpen,
  setTransferOpen,
  presignOpen,
  setPresignOpen,
  versionsOpen,
  setVersionsOpen,
  lifecycleOpen,
  setLifecycleOpen,
}: {
  scopeKey: string;
  target: S3ConsoleTarget;
  theme: string;
  classes: ReturnType<typeof connectorConsoleTheme>;
  browser: ReturnType<typeof useS3Browser>;
  upload: ReturnType<typeof useS3Upload>;
  deletion: ReturnType<typeof useS3ObjectDelete>;
  transferOpen: boolean;
  setTransferOpen: Dispatch<SetStateAction<boolean>>;
  presignOpen: boolean;
  setPresignOpen: Dispatch<SetStateAction<boolean>>;
  versionsOpen: boolean;
  setVersionsOpen: Dispatch<SetStateAction<boolean>>;
  lifecycleOpen: boolean;
  setLifecycleOpen: Dispatch<SetStateAction<boolean>>;
}) {
  return (
    <>
      <FileTransferDialog
        open={transferOpen}
        runtimeTarget={s3TransferTarget(target)}
        options={{
          transportLabel: "S3 object storage",
          defaultDirectory: "/",
          joinRemotePath: joinTransferPath,
          normalizeRemoteDirectoryInput: normalizeTransferDirectory,
          recursive: true,
          notice:
            "S3 transfers use bounded queues with multipart uploads, progress, pause, cancel, and short-lived local staging. A paused transfer resumes only while this gateway process remains running.",
          onUploadCompleted: () => browser.refreshObjects({ reset: true }),
        }}
        onClose={() => setTransferOpen(false)}
      />
      <S3PresignDialog
        open={presignOpen}
        scopeKey={scopeKey}
        selectedKey={browser.selectedKey}
        theme={theme}
        inputClass={classes.input}
        borderClass={classes.border}
        mutedClass={classes.muted}
        onClose={() => setPresignOpen(false)}
        onRun={browser.runS3Action}
      />
      <S3VersionsDialog
        open={versionsOpen}
        scopeKey={scopeKey}
        objectKey={browser.selectedKey}
        theme={theme}
        borderClass={classes.border}
        mutedClass={classes.muted}
        onClose={() => setVersionsOpen(false)}
        onRun={browser.runS3Action}
        onChanged={async (isCurrent) => {
          if (!isCurrent()) return;
          await browser.refreshObjects({ reset: true });
          if (isCurrent() && browser.selectedKey) await browser.readObjectMetadata(browser.selectedKey);
        }}
      />
      <S3LifecycleDialog
        open={lifecycleOpen}
        scopeKey={scopeKey}
        bucket={target.config?.bucket || "bucket"}
        theme={theme}
        inputClass={classes.input}
        borderClass={classes.border}
        mutedClass={classes.muted}
        onClose={() => setLifecycleOpen(false)}
        onRun={browser.runS3Action}
      />
      <S3UploadDialog
        value={upload.uploadDialog}
        theme={theme}
        inputClass={classes.input}
        borderClass={classes.border}
        mutedClass={classes.muted}
        subtlePanelClass={classes.subtlePanel}
        onClose={upload.closeUploadDialog}
        onChange={upload.setUploadDialog}
        onFiles={upload.addUploadFiles}
        onRemoveFile={upload.removeUploadFile}
        onUpdateFile={upload.updateUploadFile}
        onSubmit={upload.uploadObjects}
      />
      <S3ConfirmDialog
        value={deletion.confirmDialog}
        theme={theme}
        onClose={deletion.closeConfirmDialog}
        onConfirm={deletion.confirmPendingAction}
      />
    </>
  );
}

function s3TransferTarget(target: S3ConsoleTarget) {
  if (!target.transfer_runtime_id) return null;
  return {
    id: target.transfer_runtime_id,
    name: target.target_name || target.name || "S3 target",
    subtitle: `${target.config?.scheme || "https"}://${target.config?.host || "s3.amazonaws.com"}:${target.config?.port || 443}/${target.config?.bucket || "bucket"}`,
  };
}
