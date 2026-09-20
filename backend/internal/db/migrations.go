package db

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/db/auditmigration"
)

const (
	connectorNativeBaselineVersion     = 1
	connectorNativeBaselineDescription = "0.2 connector-native baseline"
)

var ErrUnsupportedSchema = errors.New("unsupported database schema")

const unsupportedPre02DatabaseMessage = "database uses an unsupported pre-0.2 or non-baseline schema; create a fresh 0.2 database or migrate with the one-time import tool. To migrate a 0.1.x database, run `docker compose --profile migrate up -d --build migration`, then open http://localhost:3211."

func UnsupportedSchemaMessage(err error) string {
	if !errors.Is(err, ErrUnsupportedSchema) {
		return ""
	}
	if message := unsupportedSchemaMessageInChain(err); message != "" {
		return message
	}
	return "database uses an unsupported schema"
}
func unsupportedSchemaMessageInChain(err error) string {
	if err == nil {
		return ""
	}
	prefix := ErrUnsupportedSchema.Error() + ": "
	if strings.HasPrefix(err.Error(), prefix) {
		return strings.TrimPrefix(err.Error(), prefix)
	}
	type multiUnwrapper interface{ Unwrap() []error }
	if joined, ok := err.(multiUnwrapper); ok {
		for _, nested := range joined.Unwrap() {
			if message := unsupportedSchemaMessageInChain(nested); message != "" {
				return message
			}
		}
		return ""
	}
	return unsupportedSchemaMessageInChain(errors.Unwrap(err))
}

type migration struct {
	version     int
	description string
	preflight   func(*sql.Tx) error
	statements  []string
}

func migrations() []migration {
	result := migrationBatch1To4()
	result = append(result, migrationBatch5To8()...)
	result = append(result, migrationBatch9To12()...)
	result = append(result, migrationBatch13()...)
	result = append(result, migrationBatch14To17()...)
	return append(result,
		retentionIndexMigration,
		recordEnvelopeBoundaryMigration(),
		s3UploadProjectionScrubMigration,
		recordEnvelopeWriteGuardMigration(),
		actionApprovalIntegrityMigration,
		credentialSecretRevisionMigration(),
		connectorActionExecutionClaimMigration(),
		connectorActionKeyedIdentityMigration,
		connectorActionIdempotencyTombstoneMigration,
		historyKeysetPaginationMigration,
		fileTransferStartIdempotencyMigration,
		bulkCommandIdempotencyMigration,
		backupUploadIdempotencyMigration,
		fileTransferRecoveryMigration(),
		profileRestoreIdempotencyMigration(),
		vaultActionRequestEnvelopeMigration(),
		projectScopeRevisionMigration(),
		chronologicalTimestampMigration(),
		backupUploadExpiryMigration(),
		connectorLifecycleFinalizationMigration,
	)
}

func migrationBatch1To4() []migration {
	return []migration{
		{
			version:     connectorNativeBaselineVersion,
			description: connectorNativeBaselineDescription,
			statements: sqlStatements(
				coreTableStatements,
				fileTransferTableStatements,
				historyTableStatements,
				indexStatements,
				searchIndexStatements,
			),
		},
		{
			version:     2,
			description: "backup provider metadata",
			statements: sqlStatements(
				backupProviderTableStatements,
				[]string{
					`CREATE INDEX IF NOT EXISTS idx_backup_providers_type_status ON backup_providers(provider_type, status);`,
					`CREATE INDEX IF NOT EXISTS idx_backup_records_provider_database_time ON backup_records(provider_id, database_name, backup_created_at);`,
					`CREATE INDEX IF NOT EXISTS idx_backup_records_database_time ON backup_records(database_name, backup_created_at);`,
				},
			),
		},
		{
			version:     3,
			description: "docker live console runtime surfaces",
			statements: []string{
				`INSERT INTO connector_runtime_surfaces (
				connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
			)
			SELECT p.connector_kind, p.target_id, p.id, 'live_console', p.label, 'active', datetime('now'), datetime('now')
			FROM connector_credential_profiles p
			JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind
			WHERE p.connector_kind = 'docker' AND p.status = 'active' AND t.status = 'active'
			ON CONFLICT(connector_kind, target_id, profile_id, capability_kind) DO UPDATE SET
				label = excluded.label,
				status = 'active',
				updated_at = excluded.updated_at`,
			},
		},
		{
			version:     4,
			description: "projects and token project scopes",
			statements: []string{
				`CREATE TABLE projects (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				slug TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
				`CREATE UNIQUE INDEX idx_projects_active_name ON projects(name COLLATE NOCASE) WHERE status = 'active';`,
				`CREATE UNIQUE INDEX idx_projects_slug ON projects(slug);`,
				`INSERT INTO projects (name, slug, status, created_at, updated_at)
			 VALUES ('Ungrouped', 'ungrouped', 'active', datetime('now'), datetime('now'));`,
				`ALTER TABLE connector_targets ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE RESTRICT;`,
				`UPDATE connector_targets SET project_id = (SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active');`,
				`DROP INDEX IF EXISTS idx_connector_targets_active_kind_name;`,
				`CREATE UNIQUE INDEX idx_connector_targets_active_project_kind_name ON connector_targets(project_id, connector_kind, name) WHERE status = 'active';`,
				`CREATE INDEX idx_connector_targets_project_status ON connector_targets(project_id, status, name);`,
				`CREATE TRIGGER connector_targets_project_required_insert
			 BEFORE INSERT ON connector_targets
			 WHEN NEW.project_id IS NULL
			 BEGIN
				SELECT RAISE(ABORT, 'connector target project_id is required');
			 END;`,
				`CREATE TRIGGER connector_targets_project_required_update
			 BEFORE UPDATE OF project_id ON connector_targets
			 WHEN NEW.project_id IS NULL
			 BEGIN
				SELECT RAISE(ABORT, 'connector target project_id is required');
			 END;`,
				`CREATE TABLE token_project_scopes (
				token_id INTEGER NOT NULL,
				project_id INTEGER NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY(token_id, project_id),
				FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
				FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
			);`,
				`INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
			 SELECT tok.id, p.id, 1, datetime('now'), datetime('now')
			 FROM api_tokens tok CROSS JOIN projects p WHERE p.status = 'active';`,
				`CREATE INDEX idx_token_project_scopes_enabled ON token_project_scopes(token_id, enabled, project_id);`,
				`ALTER TABLE history_entries ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL;`,
				`UPDATE history_entries
			 SET project_id = COALESCE(
				(SELECT ct.project_id FROM connector_targets ct WHERE ct.id = history_entries.target_id),
				(SELECT ct.project_id
				 FROM connector_runtime_surfaces rs
				 JOIN connector_targets ct ON ct.id = rs.target_id
				 WHERE rs.id = history_entries.runtime_id)
			 );`,
				`CREATE INDEX idx_history_entries_project_created ON history_entries(project_id, created_at);`,
				`ALTER TABLE audit_logs ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL;`,
				`UPDATE audit_logs
			 SET project_id = COALESCE(
				(SELECT ct.project_id FROM connector_targets ct WHERE ct.id = audit_logs.target_id),
				(SELECT ct.project_id
				 FROM connector_runtime_surfaces rs
				 JOIN connector_targets ct ON ct.id = rs.target_id
				 WHERE rs.id = audit_logs.runtime_id)
			 );`,
				`CREATE INDEX idx_audit_logs_project_created ON audit_logs(project_id, created_at);`,
			},
		},
	}
}

func migrationBatch5To8() []migration {
	return []migration{
		{
			version:     5,
			description: "project vault foundation",
			statements:  projectVaultTableStatements,
		},
		{
			version:     6,
			description: "principal-aware exact console sessions",
			statements: []string{
				`ALTER TABLE console_sessions ADD COLUMN generation INTEGER NOT NULL DEFAULT 0;`,
				`ALTER TABLE console_sessions ADD COLUMN principal_kind TEXT NOT NULL DEFAULT 'local_operator';`,
				`ALTER TABLE console_sessions ADD COLUMN principal_token_id INTEGER;`,
				`ALTER TABLE console_sessions ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE console_sessions ADD COLUMN runtime_instance_id TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE console_sessions ADD COLUMN environment_content_hash TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE console_sessions ADD COLUMN approval_context_hash TEXT NOT NULL DEFAULT '';`,
				`UPDATE console_sessions SET generation = id WHERE generation = 0;`,
				`CREATE UNIQUE INDEX idx_console_sessions_runtime_generation ON console_sessions(runtime_id, generation);`,
				`ALTER TABLE connector_action_requests ADD COLUMN session_id INTEGER;`,
				`ALTER TABLE connector_action_requests ADD COLUMN session_generation INTEGER;`,
				`CREATE INDEX idx_connector_action_requests_session ON connector_action_requests(session_id, session_generation);`,
			},
		},
		{
			version:     7,
			description: "Vault approvals and session enforcement",
			statements: []string{
				`ALTER TABLE vault_action_requests ADD COLUMN output_json TEXT NOT NULL DEFAULT 'null';`,
				`ALTER TABLE vault_action_requests ADD COLUMN user_note TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE vault_action_requests ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';`,
				`UPDATE vault_action_requests
			 SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', created_at, '+15 minutes')
			 WHERE expires_at = '';`,
				`CREATE INDEX idx_vault_action_requests_pending_expiry
				ON vault_action_requests(status, expires_at);`,
				`ALTER TABLE vault_session_leases ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE;`,
				`ALTER TABLE vault_session_leases ADD COLUMN environment_content_hash TEXT NOT NULL DEFAULT '';`,
				`UPDATE vault_session_leases
			 SET environment_content_hash = COALESCE(
				(SELECT environment_content_hash
				 FROM console_sessions
				 WHERE console_sessions.id = vault_session_leases.session_id),
				''
			 );`,
				`CREATE INDEX idx_vault_session_leases_project
				ON vault_session_leases(project_id, status, expires_at);`,
				`CREATE INDEX idx_vault_action_requests_project_status
				ON vault_action_requests(project_id, status, id);`,
				`CREATE INDEX idx_vault_action_requests_runtime_status
				ON vault_action_requests(runtime_id, status, id);`,
				`CREATE INDEX idx_vault_action_requests_action_status
				ON vault_action_requests(action_name, status, id);`,
				`CREATE INDEX idx_vault_session_leases_session_status
				ON vault_session_leases(session_id, session_generation, status);`,
				`CREATE TABLE vault_session_items (
				session_id INTEGER NOT NULL,
				vault_item_id INTEGER NOT NULL,
				source_project_id INTEGER NOT NULL,
				value_version INTEGER NOT NULL,
				metadata_revision INTEGER NOT NULL,
				binding_id INTEGER,
				binding_revision INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				PRIMARY KEY(session_id, vault_item_id),
				FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE,
				FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE,
				FOREIGN KEY(source_project_id) REFERENCES projects(id) ON DELETE RESTRICT,
				FOREIGN KEY(binding_id) REFERENCES vault_default_bindings(id) ON DELETE SET NULL
			);`,
				`CREATE INDEX idx_vault_session_items_item
				ON vault_session_items(vault_item_id, session_id);`,
				`CREATE INDEX idx_vault_session_items_binding
				ON vault_session_items(binding_id, session_id);`,
			},
		},
		{
			version:     8,
			description: "globally unique active Vault item names",
			// Upgrade development databases that predate the baseline's global index.
			preflight: requireGloballyUniqueVaultItemNames,
			statements: []string{
				`DROP INDEX IF EXISTS idx_vault_items_active_owner_name;`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_items_active_name
				ON vault_items(name COLLATE NOCASE)
				WHERE status = 'active';`,
			},
		},
	}
}

func migrationBatch9To12() []migration {
	return []migration{
		{
			version:     9,
			description: "immutable Vault session item snapshots",
			statements: []string{
				`CREATE TABLE vault_session_items_snapshot (
				session_id INTEGER NOT NULL,
				vault_item_id INTEGER NOT NULL,
				vault_item_name TEXT NOT NULL,
				source_project_id INTEGER NOT NULL,
				value_version INTEGER NOT NULL,
				metadata_revision INTEGER NOT NULL,
				binding_id INTEGER,
				binding_revision INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				PRIMARY KEY(session_id, vault_item_id),
				FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE,
				FOREIGN KEY(source_project_id) REFERENCES projects(id) ON DELETE RESTRICT,
				FOREIGN KEY(binding_id) REFERENCES vault_default_bindings(id) ON DELETE SET NULL
			);`,
				`INSERT INTO vault_session_items_snapshot (
				session_id, vault_item_id, vault_item_name, source_project_id,
				value_version, metadata_revision, binding_id, binding_revision, created_at
			)
			SELECT vsi.session_id, vsi.vault_item_id, vi.name, vsi.source_project_id,
			       vsi.value_version, vsi.metadata_revision, vsi.binding_id,
			       vsi.binding_revision, vsi.created_at
			FROM vault_session_items vsi
			JOIN vault_items vi ON vi.id = vsi.vault_item_id;`,
				`DROP TABLE vault_session_items;`,
				`ALTER TABLE vault_session_items_snapshot RENAME TO vault_session_items;`,
				`CREATE INDEX idx_vault_session_items_item
				ON vault_session_items(vault_item_id, session_id);`,
				`CREATE INDEX idx_vault_session_items_binding
				ON vault_session_items(binding_id, session_id);`,
			},
		},
		{
			version:     10,
			description: "self-hosted encrypted backup provider",
			statements: []string{
				`UPDATE backup_providers
			 SET status = 'archived', encrypted_secret_json = '', updated_at = datetime('now')
			 WHERE provider_type = 'google_drive'`,
			},
		},
		{
			version:     11,
			description: "connector action idempotency",
			statements: []string{
				`ALTER TABLE connector_action_requests ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE connector_action_requests ADD COLUMN idempotency_identity_hash TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE connector_action_requests ADD COLUMN idempotency_scope TEXT NOT NULL DEFAULT '';`,
				`CREATE UNIQUE INDEX idx_connector_action_requests_idempotency
				ON connector_action_requests(idempotency_scope, idempotency_key)
				WHERE idempotency_key <> '';`,
			},
		},
		{
			version:     12,
			description: "transactional audit outbox",
			statements: []string{
				`ALTER TABLE audit_logs ADD COLUMN event_id TEXT;`,
				`CREATE UNIQUE INDEX idx_audit_logs_event_id
				ON audit_logs(event_id)
				WHERE event_id IS NOT NULL;`,
				`CREATE TABLE audit_outbox (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				event_id TEXT NOT NULL UNIQUE,
				event_version INTEGER NOT NULL,
				actor_type TEXT NOT NULL,
				token_id INTEGER,
				project_id INTEGER,
				runtime_id INTEGER,
				connector_kind TEXT NOT NULL DEFAULT '',
				target_id INTEGER,
				profile_id INTEGER,
				action_request_id INTEGER,
				action TEXT NOT NULL,
				lifecycle_phase TEXT NOT NULL DEFAULT '',
				payload_json TEXT NOT NULL DEFAULT '{}',
				occurred_at TEXT NOT NULL,
				created_at TEXT NOT NULL,
				delivered_at TEXT,
				attempt_count INTEGER NOT NULL DEFAULT 0,
				last_error TEXT NOT NULL DEFAULT '',
				last_attempt_at TEXT,
				FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
				FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL,
				FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL
			);`,
				`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(delivered_at, id);`,
				`CREATE INDEX idx_audit_outbox_attempts
				ON audit_outbox(attempt_count, last_attempt_at)
				WHERE delivered_at IS NULL;`,
				`CREATE TABLE audit_dispatch_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				failure_count INTEGER NOT NULL DEFAULT 0,
				last_error TEXT NOT NULL DEFAULT '',
				last_failure_at TEXT,
				last_success_at TEXT,
				updated_at TEXT NOT NULL
			);`,
				`INSERT INTO audit_dispatch_state (id, updated_at)
				VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));`,
			},
		},
	}
}

func migrationBatch13() []migration {
	return []migration{{
		version:     13,
		description: "transactional runtime lifecycle audit transitions",
		statements:  auditmigration.LegacyTriggers(),
	}}
}

func migrationBatch14To17() []migration {
	return []migration{
		{
			version:     14,
			description: "audit delivery recovery and command lifecycle projection",
			statements: []string{
				`ALTER TABLE audit_logs ADD COLUMN event_version INTEGER NOT NULL DEFAULT 1;`,
				`ALTER TABLE audit_logs ADD COLUMN lifecycle_phase TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE audit_outbox ADD COLUMN next_attempt_at TEXT;`,
				`ALTER TABLE audit_outbox ADD COLUMN dead_lettered_at TEXT;`,
				`DROP INDEX idx_audit_outbox_pending;`,
				`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(dead_lettered_at, delivered_at, next_attempt_at, id);`,
				auditCommandRequestCreatedTrigger,
				auditCommandRequestStatusChangedTrigger,
			},
		},
		{
			version:     15,
			description: "repair Vault session lease environment binding",
			preflight:   ensureVaultSessionLeaseEnvironmentHash,
		},
		{
			version:     16,
			description: "repair audit delivery recovery schema",
			preflight:   ensureAuditRecoverySchema,
			statements: []string{
				`DROP INDEX IF EXISTS idx_audit_outbox_pending;`,
				`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(dead_lettered_at, delivered_at, next_attempt_at, id);`,
				`DROP TRIGGER IF EXISTS audit_command_request_created;`,
				`DROP TRIGGER IF EXISTS audit_command_request_status_changed;`,
				auditCommandRequestCreatedTrigger,
				auditCommandRequestStatusChangedTrigger,
			},
		},
		{
			version:     17,
			description: "structured file transfer failure outcomes",
			statements: []string{
				`ALTER TABLE file_transfers ADD COLUMN failure_kind TEXT NOT NULL DEFAULT '';`,
				`ALTER TABLE file_transfer_batches ADD COLUMN failure_kind TEXT NOT NULL DEFAULT '';`,
				`DROP TRIGGER IF EXISTS audit_file_transfer_status_changed;`,
				`CREATE TRIGGER audit_file_transfer_status_changed
			 AFTER UPDATE OF status ON file_transfers
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.' || NEW.status, NEW.status, printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","failure_kind":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction,
						NEW.source, OLD.status, NEW.status, NEW.failure_kind
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
				`DROP TRIGGER IF EXISTS audit_file_transfer_batch_status_changed;`,
				`CREATE TRIGGER audit_file_transfer_batch_status_changed
			 AFTER UPDATE OF status ON file_transfer_batches
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.' || NEW.status, NEW.status, printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","failure_kind":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, OLD.status,
						NEW.status, NEW.failure_kind, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			},
		},
	}
}

func sqlStatements(groups ...[]string) []string {
	var total int
	for _, group := range groups {
		total += len(group)
	}
	statements := make([]string, 0, total)
	for _, group := range groups {
		statements = append(statements, group...)
	}
	return statements
}
