package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/siigofiscal/go_backend/internal/domain/event"
	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// DownloadQuery handles SAT_WS_QUERY_DOWNLOAD_READY messages.
//
// Pipeline step 3: load FIEL → download each package ZIP from SAT → store to
// blob storage → update state=DOWNLOADED → publish SAT_WS_QUERY_DOWNLOADED
// (which OnQueryReadyToDownloadProcessQuery routes to metadata or XML processing).
//
// Mirrors Python QueryDownloaderWS.download.
type DownloadQuery struct {
	Deps
}

func (h *DownloadQuery) Handle(ctx context.Context, raw json.RawMessage) error {
	var msg DownloadQueryMsg
	if err := json.Unmarshal(raw, &msg); err != nil {
		return fmt.Errorf("unmarshal DownloadQueryMsg: %w", err)
	}

	logger := slog.With(
		"handler", "DownloadQuery",
		"company", msg.CompanyIdentifier,
		"query", msg.QueryIdentifier,
		"packages", len(msg.Packages),
	)

	// Load FIEL.
	connector, err := h.loadFIEL(ctx, msg.WID, msg.CID)
	if err != nil {
		logger.Error("failed to load FIEL for download", "error", err)
		return h.markDownloadError(ctx, msg, logger, err)
	}

	// Download all packages concurrently and store to blob.
	bucket := h.Cfg.S3Attachments
	var (
		mu       sync.Mutex
		firstErr error
	)

	var wg sync.WaitGroup
	for _, pkgID := range msg.Packages {
		wg.Add(1)
		go func(packageID string) {
			defer wg.Done()

			logger.Debug("downloading package", "package_id", packageID)

			pkg, dlErr := connector.DownloadPackage(packageID, mapRequestType(msg.RequestType))
			if dlErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("download package %s: %w", packageID, dlErr)
				}
				mu.Unlock()
				return
			}

			// Store ZIP to blob: attachments/Zips/{package_id}.zip
			blobKey := fmt.Sprintf("attachments/Zips/%s.zip", packageID)
			if uploadErr := h.Storage.Upload(ctx, bucket, blobKey, pkg.Binary); uploadErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("upload package %s: %w", packageID, uploadErr)
				}
				mu.Unlock()
				return
			}

			logger.Debug("package stored", "package_id", packageID, "size", len(pkg.Binary))
		}(pkgID)
	}
	wg.Wait()

	if firstErr != nil {
		logger.Error("package download/upload failed", "error", firstErr)
		return h.markDownloadError(ctx, msg, logger, firstErr)
	}

	// Update state to DOWNLOADED.
	if err := h.updateDownloadState(ctx, msg, tenant.QueryStateDownloaded); err != nil {
		return err
	}

	logger.Info("all packages downloaded")

	// Publish SAT_WS_QUERY_DOWNLOADED → routed by OnQueryReadyToDownloadProcessQuery.
	h.Bus.Publish(event.EventTypeSATWSQueryDownloaded, event.QueryDownloadedEvent{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: msg.CompanyIdentifier,
		QueryIdentifier:   msg.QueryIdentifier,
		RequestType:       msg.RequestType,
	})

	return nil
}

// markDownloadError publishes a WS_UPDATER event with ERROR state.
func (h *DownloadQuery) markDownloadError(ctx context.Context, msg DownloadQueryMsg, logger *slog.Logger, cause error) error {
	logger.Error("marking download error", "cause", cause)
	return h.updateDownloadState(ctx, msg, tenant.QueryStateError)
}

// updateDownloadState updates the sat_query row state and publishes WS_UPDATER.
func (h *DownloadQuery) updateDownloadState(ctx context.Context, msg DownloadQueryMsg, state string) error {
	conn, err := h.DB.TenantConn(ctx, msg.CompanyIdentifier, false)
	if err != nil {
		return fmt.Errorf("tenant conn: %w", err)
	}
	defer conn.Close()

	now := time.Now().UTC()
	pkgJSON, _ := json.Marshal(msg.Packages)

	if _, err := conn.NewUpdate().
		Model((*tenant.SATQuery)(nil)).
		Set("state = ?", state).
		Set("packages = ?", string(pkgJSON)).
		Set("cfdis_qty = ?", msg.CfdisQty).
		Set("updated_at = ?", now).
		Where("identifier = ?", msg.QueryIdentifier).
		Exec(ctx); err != nil {
		return fmt.Errorf("update sat_query to %s: %w", state, err)
	}

	return nil
}
