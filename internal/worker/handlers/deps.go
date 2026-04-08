package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/infra/sat"
)

// Deps bundles the shared dependencies injected into every handler.
type Deps struct {
	DB      *db.Database
	Bus     *event.Bus
	Storage port.FileStorage
	Cfg     *config.Config
}

// loadFIEL downloads the FIEL cert, key, and passphrase from blob storage and
// returns a SAT Connector ready for SOAP operations.
// S3/Blob key pattern: ws_{wid}/c_{cid}.{cer,key,txt}
func (d *Deps) loadFIEL(ctx context.Context, wid, cid int64) (*sat.Connector, error) {
	bucket := d.Cfg.S3Certs

	certKey := fmt.Sprintf("ws_%d/c_%d.cer", wid, cid)
	keyKey := fmt.Sprintf("ws_%d/c_%d.key", wid, cid)
	passKey := fmt.Sprintf("ws_%d/c_%d.txt", wid, cid)

	certDER, err := d.Storage.Download(ctx, bucket, certKey)
	if err != nil {
		return nil, &sat.CertsNotFoundError{Detail: fmt.Sprintf("download cert: %v", err)}
	}

	keyDER, err := d.Storage.Download(ctx, bucket, keyKey)
	if err != nil {
		return nil, &sat.CertsNotFoundError{Detail: fmt.Sprintf("download key: %v", err)}
	}

	passphrase, err := d.Storage.Download(ctx, bucket, passKey)
	if err != nil {
		return nil, &sat.CertsNotFoundError{Detail: fmt.Sprintf("download passphrase: %v", err)}
	}

	connector, err := sat.NewConnector(certDER, keyDER, passphrase, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("create SAT connector: %w", err)
	}

	slog.Info("worker: FIEL loaded", "wid", wid, "cid", cid, "rfc", connector.RFC)
	return connector, nil
}
