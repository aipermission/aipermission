package auditmigration

// LegacyTriggers returns the version-13 statements verbatim; applied migrations are immutable.
func LegacyTriggers() []string {
	return []string{
		`CREATE TRIGGER audit_console_session_created
			 AFTER INSERT ON console_sessions
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'console.session.created', 'created', printf(
						'{"session_id":%d,"runtime_id":%d,"generation":%d,"status":"%s"}',
						NEW.id, NEW.runtime_id, NEW.generation, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_console_session_status_changed
			 AFTER UPDATE OF status ON console_sessions
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'console.session.' || NEW.status, NEW.status, printf(
						'{"session_id":%d,"runtime_id":%d,"generation":%d,"previous_status":"%s","status":"%s"}',
						NEW.id, NEW.runtime_id, NEW.generation, OLD.status, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_file_transfer_created
			 AFTER INSERT ON file_transfers
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id, 'file_transfer.created',
					NEW.status, printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","status":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction, NEW.source, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
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
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction,
						NEW.source, OLD.status, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_file_transfer_queue_changed
			 AFTER UPDATE OF queue_index ON file_transfers
			 WHEN OLD.queue_index <> NEW.queue_index
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.queue_updated', 'updated', printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"previous_queue_index":%d,"queue_index":%d}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, OLD.queue_index, NEW.queue_index
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_file_transfer_removed
			 AFTER DELETE ON file_transfers
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, OLD.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.removed', 'deleted', printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"status":"%s"}',
						OLD.id, COALESCE(OLD.batch_id, 0), OLD.runtime_id, OLD.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = OLD.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_file_transfer_batch_created
			 AFTER INSERT ON file_transfer_batches
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.created', NEW.status, printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","status":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, NEW.status, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
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
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, OLD.status, NEW.status, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		`CREATE TRIGGER audit_file_transfer_batch_archive_ready
			 AFTER UPDATE OF archive_path ON file_transfer_batches
			 WHEN OLD.archive_path <> NEW.archive_path AND NEW.archive_path <> ''
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.archive_ready', 'completed', printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s"}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
	}
}
