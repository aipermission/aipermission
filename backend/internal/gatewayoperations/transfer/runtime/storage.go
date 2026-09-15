package transferruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func (s Runner) StageUploadFile(runtime *Runtime, reader io.Reader) (string, int64, string, error) {
	root, err := s.EnsureTempRoot(runtime)
	if err != nil {
		return "", 0, "", err
	}
	return filetransfer.StageUpload(root, reader)
}

func (s Runner) ReserveDownloadTempFile(runtime *Runtime) (string, error) {
	root, err := s.EnsureTempRoot(runtime)
	if err != nil {
		return "", err
	}
	return filetransfer.ReserveDownload(root)
}

func (s Runner) RemoveTransferTemp(runtime *Runtime, transferID int64) {
	item, err := runtime.store.Get(context.Background(), transferID)
	if err == nil {
		s.removeTransferTemp(runtime, transferID, item.TempPath)
	}
}

func (s Runner) removeTransferTemp(runtime *Runtime, transferID int64, value string) {
	if runtime == nil || value == "" || !s.TempPathAllowed(runtime, value) {
		return
	}
	if err := os.Remove(value); err != nil && !os.IsNotExist(err) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	_ = runtime.store.ClearTempPath(ctx, transferID, value)
}

func (s Runner) RemoveTempPath(runtime *Runtime, value string) {
	if value != "" && s.TempPathAllowed(runtime, value) {
		_ = os.Remove(value)
	}
}

func (s Runner) transferTerminalDurable(runtime *Runtime, transferID int64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	item, err := runtime.store.Get(ctx, transferID)
	return err == nil && fileTransferTerminal(item.Status)
}

func fileTransferTerminal(status string) bool {
	return status == filetransfer.StatusCompleted || status == filetransfer.StatusFailed || status == filetransfer.StatusCanceled
}

func (s Runner) CleanupBatchTemps(runtime *Runtime, batchID int64) {
	batch, err := runtime.store.GetBatch(context.Background(), batchID)
	if err != nil {
		return
	}
	s.RemoveTempPath(runtime, batch.ArchivePath)
	for _, item := range batch.Items {
		if item.Direction == filetransfer.DirectionUpload && item.FailureKind == filetransfer.FailureKindOutcomeUnknown {
			continue
		}
		s.removeTransferTemp(runtime, item.ID, item.TempPath)
	}
}
func (s Runner) cleanupBatchTempsIfDurable(runtime *Runtime, batchID int64, durable bool) {
	if durable {
		s.CleanupBatchTemps(runtime, batchID)
	}
}
func (s Runner) scheduleEphemeralBatchTempCleanup(runtime *Runtime, batch filetransfer.BatchRecord) {
	s.scheduleEphemeralTempCleanup(runtime, batch.ArchivePath)
	for _, item := range batch.Items {
		s.scheduleEphemeralTempCleanup(runtime, item.TempPath)
	}
}

func (s Runner) scheduleBatchTempCleanup(runtime *Runtime, batch filetransfer.BatchRecord) {
	expiresAt, err := time.Parse(time.RFC3339Nano, batch.ArchiveExpiresAt)
	if err != nil {
		expiresAt = time.Now().Add(s.tempTTL)
	}
	if batch.ArchivePath != "" {
		s.scheduleBatchArchiveCleanup(runtime, batch.ID, batch.ArchivePath, expiresAt)
	}
	for _, item := range batch.Items {
		s.scheduleTransferRecordTempCleanup(runtime, item.ID, item.TempPath, expiresAt)
	}
}

func (s Runner) scheduleBatchArchiveCleanup(runtime *Runtime, batchID int64, archivePath string, expiresAt time.Time) {
	delay := time.Until(expiresAt)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() { s.removeExpiredBatchArchive(runtime, batchID, archivePath) })
}

func (s Runner) removeExpiredBatchArchive(runtime *Runtime, batchID int64, archivePath string) {
	if runtime == nil || !s.TempPathAllowed(runtime, archivePath) {
		return
	}
	if err := os.Remove(archivePath); err != nil && !os.IsNotExist(err) {
		log.Printf("remove expired file transfer archive failed batch=%d error=%v", batchID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	if err := runtime.store.ClearBatchArchive(ctx, batchID, archivePath); err != nil {
		log.Printf("clear expired file transfer archive record failed batch=%d error=%v", batchID, err)
	}
}

func (s Runner) CreateDownloadArchive(runtime *Runtime, batch filetransfer.BatchRecord) (string, error) {
	root, err := s.EnsureTempRoot(runtime)
	if err != nil {
		return "", err
	}
	return filetransfer.CreateDownloadArchive(root, batch)
}

func (s Runner) EnsureTempRoot(runtime *Runtime) (string, error) {
	root, err := s.workspaceTempRoot(runtime)
	if err != nil {
		return "", err
	}
	if err := ensurePrivateTransferDirectory(root); err != nil {
		return "", err
	}
	return root, nil
}

func ensurePrivateTransferDirectory(path string) error {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve file transfer temp directory: %w", err)
	}
	current := filepath.VolumeName(abs) + string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, inspectErr := os.Lstat(current)
		if os.IsNotExist(inspectErr) {
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return fmt.Errorf("create file transfer temp directory: %w", err)
			}
			info, inspectErr = os.Lstat(current)
		}
		if inspectErr != nil {
			return fmt.Errorf("inspect file transfer temp directory: %w", inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("file transfer temp path must contain only directories and no symbolic links")
		}
	}
	return nil
}

func (s Runner) workspaceTempRoot(runtime *Runtime) (string, error) {
	if runtime == nil || strings.TrimSpace(runtime.storageID) == "" {
		return "", fmt.Errorf("file transfer storage identity is unavailable")
	}
	digest := sha256.Sum256([]byte(runtime.storageID))
	return filepath.Join(s.fileTransferTempRoot(), hex.EncodeToString(digest[:16])), nil
}

func (s Runner) fileTransferTempRoot() string {
	return filepath.Join(filepath.Dir(s.dataPath), "file-transfers")
}

func (s Runner) TempPathAllowed(runtime *Runtime, value string) bool {
	root, err := s.workspaceTempRoot(runtime)
	return err == nil && filetransfer.TempPathAllowed(root, value)
}

func (s Runner) scheduleEphemeralTempCleanup(runtime *Runtime, value string) {
	root, err := s.workspaceTempRoot(runtime)
	if err == nil {
		filetransfer.ScheduleTempCleanup(root, value, s.tempTTL)
	}
}

func (s Runner) cleanupTransferTempAfterError(runtime *Runtime, transferID int64, value string, err error) {
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		s.scheduleTransferRecordTempCleanup(runtime, transferID, value, time.Now().Add(s.tempTTL))
		return
	}
	s.removeTransferTemp(runtime, transferID, value)
}

func (s Runner) scheduleTransferRecordTempCleanup(runtime *Runtime, transferID int64, value string, expiresAt time.Time) {
	if runtime == nil || value == "" || !s.TempPathAllowed(runtime, value) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	err := runtime.store.SetTempExpiry(ctx, transferID, value, expiresAt)
	cancel()
	if err != nil {
		log.Printf("persist file transfer temp expiry failed transfer=%d error=%v", transferID, err)
	}
	delay := time.Until(expiresAt)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() { s.removeExpiredTransferTemp(runtime, transferID, value) })
}

func (s Runner) removeExpiredTransferTemp(runtime *Runtime, transferID int64, value string) {
	if runtime == nil || !s.TempPathAllowed(runtime, value) {
		return
	}
	if err := os.Remove(value); err != nil && !os.IsNotExist(err) {
		log.Printf("remove expired file transfer temp failed transfer=%d error=%v", transferID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fileTransferPersistenceAttemptTimeout)
	defer cancel()
	if err := runtime.store.ClearTempPath(ctx, transferID, value); err != nil {
		log.Printf("clear expired file transfer temp record failed transfer=%d error=%v", transferID, err)
	}
}

func (s Runner) RecoverTempCleanup(ctx context.Context, runtime *Runtime) error {
	if runtime == nil {
		return fmt.Errorf("file transfer runtime is unavailable")
	}
	var cleanupErrors []error
	if err := s.scavengeOrphanedTempFiles(ctx, runtime); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	items, err := runtime.store.ListTempCleanupCandidates(ctx)
	if err != nil {
		return errors.Join(append(cleanupErrors, err)...)
	}
	for _, item := range items {
		if !s.TempPathAllowed(runtime, item.TempPath) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("file transfer %d has an unsafe temp path", item.ID))
			continue
		}
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, item.TempExpiresAt)
		if parseErr != nil {
			info, statErr := os.Stat(item.TempPath)
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect file transfer temp %d: %w", item.ID, statErr))
				continue
			}
			if errors.Is(statErr, os.ErrNotExist) {
				s.removeExpiredTransferTemp(runtime, item.ID, item.TempPath)
				continue
			}
			expiresAt = info.ModTime().Add(s.tempTTL)
			if err := runtime.store.SetTempExpiry(ctx, item.ID, item.TempPath, expiresAt); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("repair file transfer temp expiry %d: %w", item.ID, err))
				continue
			}
		}
		if !expiresAt.After(time.Now()) {
			s.removeExpiredTransferTemp(runtime, item.ID, item.TempPath)
		}
	}
	archives, err := runtime.store.ListBatchArchiveCleanupCandidates(ctx)
	if err != nil {
		return errors.Join(append(cleanupErrors, err)...)
	}
	for _, batch := range archives {
		if !s.TempPathAllowed(runtime, batch.ArchivePath) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("file transfer batch %d has an unsafe archive path", batch.ID))
			continue
		}
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, batch.ArchiveExpiresAt)
		if parseErr != nil {
			info, statErr := os.Stat(batch.ArchivePath)
			if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect file transfer archive %d: %w", batch.ID, statErr))
				continue
			}
			if errors.Is(statErr, os.ErrNotExist) {
				s.removeExpiredBatchArchive(runtime, batch.ID, batch.ArchivePath)
				continue
			}
			expiresAt = info.ModTime().Add(s.tempTTL)
		}
		if !expiresAt.After(time.Now()) {
			s.removeExpiredBatchArchive(runtime, batch.ID, batch.ArchivePath)
		}
	}
	return errors.Join(cleanupErrors...)
}

func (s Runner) StartTempCleanupRecovery(runtime *Runtime) bool {
	if runtime == nil || runtime.jobs == nil || !runtime.finalization.Valid() || s.tempTTL <= 0 {
		return false
	}
	ctx, cancel := context.WithCancel(runtime.finalization.Context())
	return runtime.jobs.Maintenance.Launch(2, cancel, func() {
		ticker := time.NewTicker(s.tempCleanupRetry)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.RecoverTempCleanup(ctx, runtime); err != nil && !errors.Is(err, context.Canceled) {
					log.Printf("recover local file transfer staging delayed: %v", err)
				}
			}
		}
	})
}

func (s Runner) StartRemoteStagingRecovery(runtime *Runtime) bool {
	if runtime == nil || runtime.jobs == nil || !runtime.finalization.Valid() {
		return false
	}
	ctx, cancel := context.WithCancel(runtime.finalization.Context())
	return runtime.jobs.Maintenance.Launch(1, cancel, func() {
		for {
			pending, err := s.recoverRemoteStagingPass(ctx, runtime)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Printf("recover remote file transfer staging delayed: %v", err)
				pending = true
			}
			if !pending {
				return
			}
			timer := time.NewTimer(s.remoteRecoveryRetry)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}

func (s Runner) RecoverRemoteStaging(ctx context.Context, runtime *Runtime) error {
	if runtime == nil {
		return fmt.Errorf("file transfer runtime is unavailable")
	}
	_, err := s.recoverRemoteStagingPass(ctx, runtime)
	return err
}

func (s Runner) scavengeOrphanedTempFiles(ctx context.Context, runtime *Runtime) error {
	if s.tempTTL <= 0 {
		return nil
	}
	references, err := runtime.store.ListManagedTempPaths(ctx)
	if err != nil {
		return err
	}
	root, err := s.workspaceTempRoot(runtime)
	if err != nil {
		return err
	}
	if err := ensurePrivateTransferDirectory(root); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect file transfer temp directory: %w", err)
	}
	cutoff := time.Now().Add(-s.tempTTL)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !ownedFileTransferTempName(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if _, retained := references[filepath.Clean(path)]; retained {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspect orphaned file transfer temp: %w", err)
		}
		if !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove orphaned file transfer temp: %w", err)
		}
	}
	return nil
}

func ownedFileTransferTempName(name string) bool {
	return strings.HasPrefix(name, "upload-") || strings.HasPrefix(name, "download-") ||
		(strings.HasPrefix(name, "archive-") && strings.HasSuffix(name, ".zip"))
}

func (s Runner) recoverRemoteStagingPass(ctx context.Context, runtime *Runtime) (bool, error) {
	items, err := runtime.store.ListRemoteStagingCandidates(ctx)
	if err != nil {
		return false, err
	}
	pending := false
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		candidateCtx, cancel := context.WithTimeout(ctx, s.remoteRecoveryTimeout)
		ports, err := runtime.ConnectorPorts(candidateCtx, item.RuntimeID)
		if err != nil || s.adapterFor == nil {
			cancel()
			pending = true
			continue
		}
		cleaner, ok := s.adapterFor(ports.ConnectorKind).(connectorapi.RemoteStagingRecoveryAdapter)
		if !ok || cleaner == nil {
			cancel()
			pending = true
			continue
		}
		err = cleaner.CleanupRemoteStaging(candidateCtx, ports.Gateway, ports.Runtime, item.RuntimeID, item.RemoteStagingRef)
		cancel()
		if err != nil {
			if err := ctx.Err(); err != nil {
				return true, err
			}
			log.Printf("recover remote file transfer staging failed transfer=%d", item.ID)
			pending = true
			continue
		}
		if err := runtime.store.ClearRemoteStagingRef(ctx, item.ID, item.RemoteStagingRef); err != nil {
			return true, err
		}
	}
	return pending, nil
}
