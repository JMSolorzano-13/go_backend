package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Scraper struct {
	cfg      *config.Config
	database *db.Database
	bus      *event.Bus
	files    port.FileStorage
}

func NewScraper(cfg *config.Config, database *db.Database, bus *event.Bus, files port.FileStorage) *Scraper {
	return &Scraper{cfg: cfg, database: database, bus: bus, files: files}
}

// POST /api/Scraper/scrap_sat_pdf
func (h *Scraper) ScrapSatPDF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	documentType, _ := body["document_type"].(string)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	setScraperStatus(ctx, database, cid, documentType, "pending")

	sqsDomain := map[string]interface{}{
		"company_identifier": cid,
		"document_type":      documentType,
	}

	h.bus.Publish(event.EventTypeSATScrapPDF, event.SQSMessagePayload{
		SQSBase:           event.NewSQSBase(),
		CompanyIdentifier: cid,
		JSONBody:          sqsDomain,
	})

	response.WriteJSON(w, http.StatusOK, sqsDomain)
}

// POST /api/Scraper/get_pdf_files
func (h *Scraper) GetPDFFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	cid, _ := body["company_identifier"].(string)
	documentType, _ := body["document_type"].(string)

	docRequested := "oc"
	if documentType == "constancy" {
		docRequested = "cf"
	}

	bucket := h.cfg.S3FilesAttach
	key := fmt.Sprintf("%s_%s.pdf", docRequested, cid)
	expiry := 8 * time.Hour

	contentURL, err := h.files.PresignGet(ctx, bucket, key, expiry)
	if err != nil {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"url_pdf_content":  "",
			"url_pdf_download": "",
			"last_update":      "",
			"error":            err.Error(),
		})
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"url_pdf_content":  contentURL,
		"url_pdf_download": contentURL,
		"last_update":      time.Now().UTC().Format(crud.APITimestampFormat),
		"error":            "",
	})
}

func setScraperStatus(ctx context.Context, database *db.Database, cid, documentType, status string) {
	key := fmt.Sprintf("scrap_status_%s", documentType)
	statusData := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now().UTC().Format(crud.APITimestampFormat),
	}
	statusJSON, _ := json.Marshal(statusData)

	var company control.Company
	err := database.Primary.NewSelect().
		Model(&company).
		Where("identifier = ?", cid).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return
	}

	var data map[string]interface{}
	if len(company.Data) > 0 {
		_ = json.Unmarshal(company.Data, &data)
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	var s interface{}
	_ = json.Unmarshal(statusJSON, &s)
	data[key] = s

	updatedData, _ := json.Marshal(data)
	_, _ = database.Primary.NewUpdate().
		Model((*control.Company)(nil)).
		Set("data = ?", string(updatedData)).
		Where("identifier = ?", cid).
		Exec(ctx)
}
