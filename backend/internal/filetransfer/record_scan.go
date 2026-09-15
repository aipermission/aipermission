package filetransfer

const transferSelect = `
	SELECT ft.id, COALESCE(ft.batch_id, 0), ft.queue_index, ft.runtime_id, COALESCE(ct.name, ''),
		ft.direction, ft.source, ft.status, ft.local_path, ft.remote_path, ft.file_name,
		ft.size_bytes, ft.transferred_bytes, ft.bytes_per_second, ft.eta_seconds,
		ft.checksum_sha256, ft.temp_path, ft.error, ft.failure_kind,
		ft.failure_details_json, COALESCE(ft.temp_expires_at, ''), ft.remote_staging_ref, ft.created_at,
		COALESCE(ft.started_at, ''), COALESCE(ft.completed_at, ''), ft.updated_at
	FROM file_transfers ft
	LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
	LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind`

type transferScanner interface {
	Scan(...any) error
}

func scanTransfer(scanner transferScanner) (Record, error) {
	var item Record
	var details failureDetailsValue
	err := scanner.Scan(
		&item.ID,
		&item.BatchID,
		&item.QueueIndex,
		&item.RuntimeID,
		&item.TargetName,
		&item.Direction,
		&item.Source,
		&item.Status,
		&item.LocalPath,
		&item.RemotePath,
		&item.FileName,
		&item.SizeBytes,
		&item.TransferredBytes,
		&item.BytesPerSecond,
		&item.ETASeconds,
		&item.ChecksumSHA256,
		&item.TempPath,
		&item.Error,
		&item.FailureKind,
		&details,
		&item.TempExpiresAt,
		&item.RemoteStagingRef,
		&item.CreatedAt,
		&item.StartedAt,
		&item.CompletedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return Record{}, err
	}
	item.FailureDetails = details.value
	return item, nil
}
