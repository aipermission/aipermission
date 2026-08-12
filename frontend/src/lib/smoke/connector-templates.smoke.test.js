import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  connectorTemplatesDir,
  shellSource,
  connectorsSource,
  credentialsSource,
  connectorTemplateCommonSource,
  kafkaConsoleSource,
  kafkaWriteDialogsSource,
  connectorTargetProfileSaveSource,
  connectorTemplateRegistrySource,
  connectorTemplateCatalogSource,
  connectorHostPingSource,
  s3PresignDialogSource,
  s3VersionsDialogSource,
  s3LifecycleDialogSource,
  backendConnectorRegistrySource,
  connectorTemplateKinds,
  sshConnectorFormTemplateSource,
  sshCredentialFormTemplateSource,
  sshCredentialRowActionsTemplateSource,
  sshConnectorListItemTemplateSource,
  sshConnectorConsoleTemplateSource,
  sshConnectorIndexSource,
  sshConnectorMetadataSource,
  sshConnectorModelSource,
  sshConnectorOperationsSource,
  postgresConnectorFormTemplateSource,
  postgresCredentialFormTemplateSource,
  postgresConnectorListItemTemplateSource,
  postgresConnectorConsoleTemplateSource,
  sharedNetworkTransportSource,
  sharedDatabaseConnectorModelSource,
  postgresConnectorIndexSource,
  postgresConnectorMetadataSource,
  postgresConnectorModelSource,
  postgresConnectorOperationsSource,
  clickHouseConnectorFormSource,
  clickHouseConnectorConsoleSource,
  clickHouseConnectorIndexSource,
  clickHouseConnectorMetadataSource,
  clickHouseConnectorModelSource,
  redisConnectorFormTemplateSource,
  rabbitMQConnectorFormTemplateSource,
  backendRegisteredConnectorKinds,
} from "./app-smoke-fixtures.js";

test("Connectors page wires generic connector templates", () => {
  assert.match(connectorsSource, /Add connector/);
  assert.match(connectorsSource, /Connector type/);
  assert.match(connectorsSource, /Operations/);
  assert.match(connectorsSource, /showUnderConstruction/);
  assert.match(connectorsSource, /getConnectorTemplate\(form\.connector_kind\)\?\.Form/);
  assert.match(connectorsSource, /getConnectorModel\(target\.connector_kind\)/);
  assert.match(connectorsSource, /model\.save/);
  assert.match(connectorsSource, /model\.deleteTarget/);
  assert.match(connectorsSource, /model\.test/);
  assert.match(connectorTemplateRegistrySource, /import\.meta\.glob\("\.\/\*\/index\.jsx"/);
  assert.match(connectorTemplateCatalogSource, /import\.meta\.glob\("\.\/\*\/metadata\.json"/);
  assert.doesNotMatch(connectorTemplateRegistrySource, /ssh:\s*Object\.freeze|postgres:\s*Object\.freeze/);
  assert.match(connectorTemplateCatalogSource, /supportedConnectorKinds/);
  assert.match(connectorTemplateRegistrySource, /templateModules/);
  assert.match(connectorTemplateRegistrySource, /connectorKindFromPath/);
  assert.match(sshConnectorIndexSource, /CredentialForm/);
  assert.match(sshConnectorIndexSource, /ToolbarActions/);
  assert.match(postgresConnectorIndexSource, /CredentialForm/);
  assert.match(postgresConnectorIndexSource, /Operations/);
  assert.match(postgresConnectorIndexSource, /ToolbarActions/);
  assert.match(sshConnectorMetadataSource, /"label": "SSH"/);
  assert.match(sshConnectorMetadataSource, /"icon": "server"/);
  assert.match(postgresConnectorMetadataSource, /"label": "Postgres"/);
  assert.match(postgresConnectorMetadataSource, /"icon": "database"/);
  assert.match(clickHouseConnectorIndexSource, /ClickHouseConnectorConsoleTemplate/);
  assert.match(clickHouseConnectorMetadataSource, /"label": "ClickHouse"/);
  assert.match(clickHouseConnectorMetadataSource, /"version": "0\.1"/);
  assert.match(clickHouseConnectorFormSource, /NetworkTransportFields/);
  assert.match(clickHouseConnectorConsoleSource, /SQLConnectorConsole/);
  assert.match(clickHouseConnectorConsoleSource, /query_readonly/);
  assert.match(clickHouseConnectorConsoleSource, /describe_table/);
  assert.match(clickHouseConnectorModelSource, /createDatabaseConnectorModel/);
  assert.match(sharedDatabaseConnectorModelSource, /createTargetWithProfile/);
  assert.match(sharedDatabaseConnectorModelSource, /updateTargetWithProfile/);
  assert.match(connectorTemplateRegistrySource, /ConnectorTemplateNotFound/);
  assert.match(sshConnectorFormTemplateSource, /SSHConnectorFormTemplate/);
  assert.match(sshConnectorFormTemplateSource, /HostPingButton/);
  assert.match(sshConnectorListItemTemplateSource, /SSHConnectorRowActionsTemplate/);
  assert.match(sshConnectorConsoleTemplateSource, /SSHConnectorConsoleTemplate/);
  assert.match(sshConnectorModelSource, /apiPost\("\/api\/connector-targets\/test"/);
  assert.match(sshConnectorModelSource, /createTargetWithProfile/);
  assert.match(sshConnectorModelSource, /updateTargetWithProfile/);
  assert.match(sshConnectorModelSource, /apiDelete\(`\/api\/connector-targets\/\$\{target\.id\}/);
  assert.doesNotMatch(sshConnectorModelSource, /\/api\/servers\/test-connection/);
  assert.match(sshConnectorModelSource, /\/profiles\/\$\{selectedProfile\.id\}\/test/);
  assert.doesNotMatch(sshConnectorModelSource, /apiPut\(`\/api\/servers\//);
  assert.doesNotMatch(sshConnectorModelSource, /apiDelete\(`\/api\/servers\//);
  assert.match(sshConnectorModelSource, /deleteDialog/);
  assert.match(postgresConnectorFormTemplateSource, /PostgresConnectorFormTemplate/);
  assert.match(postgresConnectorFormTemplateSource, /NetworkTransportFields/);
  assert.match(sharedNetworkTransportSource, /HostPingButton/);
  assert.match(sharedNetworkTransportSource, /transport_target_ref/);
  assert.match(redisConnectorFormTemplateSource, /HostPingButton/);
  assert.match(rabbitMQConnectorFormTemplateSource, /HostPingButton/);
  assert.match(connectorHostPingSource, /\/api\/connector-targets\/ping/);
  assert.match(connectorHostPingSource, /transport_target_ref/);
  assert.match(postgresConnectorListItemTemplateSource, /PostgresConnectorRowActionsTemplate/);
  assert.match(postgresConnectorConsoleTemplateSource, /PostgresConnectorConsoleTemplate/);
  assert.match(postgresConnectorModelSource, /createDatabaseConnectorModel/);
  assert.match(sharedDatabaseConnectorModelSource, /createTargetWithProfile/);
  assert.match(sharedDatabaseConnectorModelSource, /updateTargetWithProfile/);
  assert.match(sharedDatabaseConnectorModelSource, /apiDelete\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.match(sharedDatabaseConnectorModelSource, /\/profiles\/\$\{selectedProfile\.id\}\/test/);
  assert.match(sharedDatabaseConnectorModelSource, /deleteDialog/);
  assert.match(connectorTargetProfileSaveSource, /apiPost\("\/api\/connector-targets\/with-profile"/);
  assert.match(connectorTargetProfileSaveSource, /apiPut\(`\/api\/connector-targets\/\$\{targetID\}\/with-profile\/\$\{profileID\}`/);
  assert.doesNotMatch(connectorTargetProfileSaveSource, /apiDelete/);
  assert.match(sshConnectorModelSource, /function targetEndpoint/);
  assert.match(postgresConnectorModelSource, /targetEndpoint:/);
  assert.match(connectorsSource, /model\?\.targetEndpoint/);
  assert.doesNotMatch(connectorTemplateCommonSource, /target\.connector_kind ===/);
  assert.doesNotMatch(connectorTemplateCommonSource, /kind === "ssh"|kind === "postgres"/);
  assert.match(sshConnectorListItemTemplateSource, /Check Docker/);
  assert.match(sshConnectorListItemTemplateSource, /Install key/);
  assert.match(sshConnectorListItemTemplateSource, /Install key for/);
  assert.match(postgresConnectorListItemTemplateSource, /Create managed DB user/);
  assert.match(postgresConnectorListItemTemplateSource, /Backup \/ restore database/);
  assert.doesNotMatch(postgresConnectorListItemTemplateSource, /Export table JSON/);
  assert.match(postgresConnectorOperationsSource, /\/profiles\/\$\{profileID\}\/provision/);
  assert.match(postgresConnectorOperationsSource, /\/backup/);
  assert.match(postgresConnectorOperationsSource, /\/restore/);
  assert.doesNotMatch(postgresConnectorOperationsSource, /export_table_json/);
  assert.match(postgresConnectorOperationsSource, /Create user/);
  assert.doesNotMatch(sshConnectorListItemTemplateSource, /Delete SSH connector|Edit SSH connector|Test SSH/);
  assert.match(connectorsSource, /title="Test connection"/);
  assert.match(connectorsSource, /title="Edit connector"/);
  assert.match(connectorsSource, /title="Delete connector"/);
  assert.match(connectorsSource, /\/api\/connectors\/\$\{item\.kind\}/);
  assert.match(connectorsSource, /\/api\/connector-targets\/inventory/);
  assert.match(credentialsSource, /\/api\/connector-targets\/inventory/);
  assert.doesNotMatch(`${connectorsSource}\n${credentialsSource}`, /apiGet\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.doesNotMatch(connectorsSource, /connection_mode:\s*"direct"/);
  assert.doesNotMatch(connectorsSource, /apiPut\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.doesNotMatch(connectorsSource, /apiPut\(`\/api\/servers\/\$\{runtimeID\}`/);
  assert.doesNotMatch(connectorsSource, /\/api\/servers\//);
  assert.doesNotMatch(connectorsSource, /\/api\/ssh-host-keys/);
  assert.doesNotMatch(connectorsSource, /\/profiles\/\$\{profile\.id\}\/test/);
  assert.doesNotMatch(shellSource, /apiGet\("\/api\/servers"\)/);
  assert.doesNotMatch(shellSource, /\/api\/connectors\/ssh\/credentials/);
  assert.doesNotMatch(shellSource, /ssh_key_id|target\.config\?\.host|target\.config\?\.username/);
  assert.match(shellSource, /loadCredentialResources/);
  assert.match(sshConnectorModelSource, /loadCredentialResources/);
  assert.match(sshConnectorModelSource, /liveConsoleRuntimeTarget/);
  assert.match(shellSource, /liveConsoleRuntimeTargets/);
});

test("Connector template folders are registered in catalog and registry", () => {
  assert.ok(connectorTemplateKinds.includes("postgres"));
  assert.ok(connectorTemplateKinds.includes("ssh"));
  assert.deepEqual(backendRegisteredConnectorKinds(backendConnectorRegistrySource), connectorTemplateKinds);
  for (const kind of connectorTemplateKinds) {
    const indexSource = readFileSync(join(connectorTemplatesDir, kind, "index.jsx"), "utf8");
    const metadataSource = readFileSync(join(connectorTemplatesDir, kind, "metadata.json"), "utf8");
    assert.match(indexSource, /export default Object\.freeze/);
    assert.match(metadataSource, /"version"/);
  }
});

test("Kafka write dialogs guard stale detail and pending submissions", () => {
  assert.match(kafkaConsoleSource, /detailMatchesSelection/);
  assert.match(kafkaConsoleSource, /setDetailIdentity\(""\)/);
  assert.match(kafkaConsoleSource, /runGuardedConnectorAction/);
  assert.match(kafkaConsoleSource, /onRefreshActivity/);
  assert.match(kafkaWriteDialogsSource, /max-h-\[calc\(100dvh-2rem\)\]/);
  assert.match(kafkaWriteDialogsSource, /role="alert"/);
  assert.match(kafkaWriteDialogsSource, /disabled=\{pending\}/);
});

test("SSH connector template owns SSH-specific operations", () => {
  assert.match(sshConnectorModelSource, /unknown_ssh_host_key/);
  assert.match(sshConnectorModelSource, /changed_ssh_host_key/);
  assert.match(sshConnectorOperationsSource, /Replace trusted fingerprint/);
  assert.match(sshConnectorOperationsSource, /Previously trusted/);
  assert.match(sshConnectorOperationsSource, /replace: Boolean\(hostKey\.changed\)/);
  assert.match(sshConnectorOperationsSource, /\/api\/ssh-host-keys\/approve/);
  assert.match(sshConnectorOperationsSource, /Check Docker/);
  assert.match(sshConnectorOperationsSource, /Container details/);
  assert.match(sshConnectorOperationsSource, /Container logs/);
  assert.match(sshConnectorOperationsSource, /No running Docker containers/);
  assert.match(sshConnectorModelSource, /\/api\/connector-targets\/\$\{target\.id\}\/operations\/docker-check/);
  assert.match(sshConnectorModelSource, /\/api\/connector-targets\/\$\{target\.id\}\/operations\/docker-logs/);
  assert.doesNotMatch(sshConnectorModelSource, /\/api\/servers\/\$\{server\.id\}\/docker/);
  assert.match(sshConnectorFormTemplateSource, /Advanced SSH startup/);
  assert.match(sshConnectorFormTemplateSource, /startup_input_after_connect/);
  assert.match(sshConnectorFormTemplateSource, /force_shell_command/);
  assert.match(sshConnectorFormTemplateSource, /Startup input after connect/);
  assert.match(sshConnectorFormTemplateSource, /Force shell command/);
  assert.match(sshConnectorFormTemplateSource, /QNAP/);
});

test("S3 management dialogs expose bounded capability controls", () => {
  assert.match(s3PresignDialogSource, /required_headers/);
  assert.match(s3PresignDialogSource, /Required request headers/);
  assert.match(s3VersionsDialogSource, /next_cursor/);
  assert.match(s3LifecycleDialogSource, /maxLength=\{1024\}/);
});

test("Credentials page supports explicit private key import", () => {
  assert.match(credentialsSource, /Add credential/);
  assert.match(credentialsSource, /Operations/);
  assert.match(credentialsSource, /Actions/);
  assert.match(credentialsSource, /getConnectorTemplate\(drawer\.kind\)\?\.CredentialForm/);
  assert.match(credentialsSource, /model\.saveCredential/);
  assert.match(credentialsSource, /model\.deleteCredential/);
  assert.match(sshConnectorModelSource, /\/api\/connectors\/ssh\/credentials\/import/);
  assert.match(sshConnectorModelSource, /apiPut\(`\/api\/connectors\/ssh\/credentials\/\$\{row\.id\}`/);
  assert.match(postgresConnectorModelSource, /createDatabaseConnectorModel/);
  assert.match(sharedDatabaseConnectorModelSource, /\/api\/connector-targets\/\$\{form\.target_id\}\/profiles/);
  assert.match(sharedDatabaseConnectorModelSource, /apiPut\(`\/api\/connector-targets\/\$\{form\.target_id\}\/profiles\/\$\{row\.id\}`/);
  assert.doesNotMatch(credentialsSource, /apiPost|apiPut|apiDelete/);
  assert.match(credentialsSource, /Edit credential/);
  assert.match(sshCredentialFormTemplateSource, /formMode === "edit"/);
  assert.match(sshCredentialFormTemplateSource, /Save SSH credential/);
  assert.match(sshCredentialFormTemplateSource, /Import credential/);
  assert.match(sshCredentialFormTemplateSource, /Choose key file/);
  assert.match(sshCredentialFormTemplateSource, /type="file" onChange=\{onReadImportFile\}/);
  assert.match(sshCredentialFormTemplateSource, /privateKeyPlaceholder/);
  assert.match(sshCredentialFormTemplateSource, /The passphrase is not\s+saved/);
  assert.match(sshCredentialRowActionsTemplateSource, /install_command/);
  assert.match(credentialsSource, /CredentialRowActionsTemplate \? <CredentialRowActionsTemplate row=\{row\}/);
  assert.doesNotMatch(credentialsSource, /CopyButton/);
  assert.match(postgresCredentialFormTemplateSource, /Create Postgres credential/);
  assert.match(postgresCredentialFormTemplateSource, /Save Postgres credential/);
  assert.match(postgresCredentialFormTemplateSource, /Leave blank to keep current password/);
  assert.match(postgresCredentialFormTemplateSource, /Select Postgres target/);
  assert.match(postgresCredentialFormTemplateSource, /Password/);
});
