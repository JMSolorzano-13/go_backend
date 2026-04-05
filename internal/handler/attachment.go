package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/crud"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/model/tenant"
	"github.com/siigofiscal/go_backend/internal/response"
)

const (
	maxAttachmentSize     = 10 * 1024 * 1024 // 10 MB
	uploadURLExpiration   = 15 * time.Minute
	downloadURLExpiration = 15 * time.Minute
)

type Attachment struct {
	cfg      *config.Config
	database *db.Database
	files    port.FileStorage
}

func NewAttachment(cfg *config.Config, database *db.Database, files port.FileStorage) *Attachment {
	return &Attachment{cfg: cfg, database: database, files: files}
}

var attachmentMeta = crud.ModelMeta{
	DefaultOrderBy: "created_at DESC",
}

func validateFileName(name string) error {
	if name == "" {
		return fmt.Errorf("file name cannot be empty")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("file name cannot contain null bytes")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("file name cannot contain path separators")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("file name cannot be '.' or '..'")
	}
	if path.Base(name) != name {
		return fmt.Errorf("file name contains invalid characters")
	}
	return nil
}

func (h *Attachment) Search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cid, _ := auth.CompanyIdentifierFromContext(ctx)
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, cid, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	params, _, err := crud.ParseSearchBodyJSON(raw)
	if err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	result, err := crud.Search[tenant.Attachment](ctx, conn, params, attachmentMeta)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("search: %v", err))
		return
	}
	response.WriteJSON(w, http.StatusOK, result)
}

func (h *Attachment) CreateMany(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyIdentifier := r.PathValue("company_identifier")
	cfdiUUID := r.PathValue("uuid")
	user, _ := auth.UserFromContext(ctx)

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}
	var body struct {
		Items []struct {
			FileName    string `json:"file_name"`
			Size        int64  `json:"size"`
			ContentHash string `json:"content_hash"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		response.BadRequest(w, "invalid JSON")
		return
	}

	namesSeen := make(map[string]bool, len(body.Items))
	for _, item := range body.Items {
		if err := validateFileName(item.FileName); err != nil {
			response.BadRequest(w, err.Error())
			return
		}
		if namesSeen[item.FileName] {
			response.BadRequest(w, "duplicate file_name found in attachments data")
			return
		}
		namesSeen[item.FileName] = true
	}

	var cfdi tenant.CFDI
	err = conn.NewSelect().Model(&cfdi).Where(`"UUID" = ?`, cfdiUUID).Scan(ctx)
	if err != nil {
		response.NotFound(w, fmt.Sprintf("CFDI with UUID %s does not exist", cfdiUUID))
		return
	}

	var existingAttachments []tenant.Attachment
	_ = conn.NewSelect().Model(&existingAttachments).
		Where("cfdi_uuid = ?", cfdiUUID).
		Where("state != ?", tenant.AttachmentStateDeleted).
		Scan(ctx)

	existingNames := make(map[string]bool, len(existingAttachments))
	var existingSize int64
	for _, a := range existingAttachments {
		existingNames[a.FileName] = true
		existingSize += a.Size
	}

	var dups []string
	var newSize int64
	for _, item := range body.Items {
		if existingNames[item.FileName] {
			dups = append(dups, item.FileName)
		}
		newSize += item.Size
	}
	if len(dups) > 0 {
		response.BadRequest(w, fmt.Sprintf("attachments with file_name(s) %s already exist for CFDI %s",
			strings.Join(dups, ", "), cfdiUUID))
		return
	}

	totalSize := existingSize + newSize
	if totalSize > maxAttachmentSize {
		response.BadRequest(w, fmt.Sprintf("total attachments size (%d bytes) exceeds the maximum allowed limit (%d bytes)",
			totalSize, maxAttachmentSize))
		return
	}

	now := time.Now().UTC()
	urls := make(map[string]string, len(body.Items))

	for _, item := range body.Items {
		s3Key := fmt.Sprintf("%s/%s/%s", companyIdentifier, cfdiUUID, item.FileName)
		att := tenant.Attachment{
			Identifier:        crud.NewIdentifier(),
			CfdiUUID:          cfdiUUID,
			CreatorIdentifier: user.Identifier,
			Size:              item.Size,
			FileName:          item.FileName,
			ContentHash:       item.ContentHash,
			S3Key:             s3Key,
			State:             tenant.AttachmentStatePending,
			CreatedAt:         now,
		}
		if _, err := conn.NewInsert().Model(&att).Exec(ctx); err != nil {
			response.InternalError(w, fmt.Sprintf("insert attachment: %v", err))
			return
		}

		uploadURL, err := h.files.PresignPut(ctx, h.cfg.S3FilesAttach, s3Key, uploadURLExpiration)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("presign upload: %v", err))
			return
		}
		urls[item.FileName] = uploadURL
	}

	response.WriteJSON(w, http.StatusOK, urls)
}

func (h *Attachment) GetDownloadURLs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	companyIdentifier := r.PathValue("company_identifier")
	cfdiUUID := r.PathValue("uuid")

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, companyIdentifier, true)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	var cfdi tenant.CFDI
	err = conn.NewSelect().Model(&cfdi).Where(`"UUID" = ?`, cfdiUUID).Scan(ctx)
	if err != nil {
		response.NotFound(w, fmt.Sprintf("CFDI with UUID %s does not exist", cfdiUUID))
		return
	}

	var attachments []tenant.Attachment
	_ = conn.NewSelect().Model(&attachments).
		Where("cfdi_uuid = ?", cfdiUUID).
		Where("state != ?", tenant.AttachmentStateDeleted).
		Scan(ctx)

	urls := make(map[string]string, len(attachments))
	for _, att := range attachments {
		dlURL, err := h.files.PresignGet(ctx, h.cfg.S3FilesAttach, att.S3Key, downloadURLExpiration)
		if err != nil {
			response.InternalError(w, fmt.Sprintf("presign download: %v", err))
			return
		}
		urls[att.FileName] = dlURL
	}

	response.WriteJSON(w, http.StatusOK, urls)
}

func (h *Attachment) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfdiUUID := r.PathValue("uuid")
	rawFileName := r.PathValue("file_name")
	fileName, _ := url.PathUnescape(rawFileName)
	user, _ := auth.UserFromContext(ctx)
	companyIdentifier := r.PathValue("company_identifier")

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	conn, err := database.TenantConn(ctx, companyIdentifier, false)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("tenant session: %v", err))
		return
	}
	defer conn.Close()

	if err := validateFileName(fileName); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	var cfdi tenant.CFDI
	err = conn.NewSelect().Model(&cfdi).Where(`"UUID" = ?`, cfdiUUID).Scan(ctx)
	if err != nil {
		response.NotFound(w, fmt.Sprintf("CFDI with UUID %s does not exist", cfdiUUID))
		return
	}

	var att tenant.Attachment
	err = conn.NewSelect().Model(&att).
		Where("cfdi_uuid = ?", cfdiUUID).
		Where("state != ?", tenant.AttachmentStateDeleted).
		Where("file_name = ?", fileName).
		Scan(ctx)
	if err != nil {
		response.NotFound(w, fmt.Sprintf("attachment with file_name %s does not exist for CFDI %s", fileName, cfdiUUID))
		return
	}

	_ = h.files.Delete(ctx, h.cfg.S3FilesAttach, att.S3Key)

	now := time.Now().UTC()
	_, err = conn.NewUpdate().Model((*tenant.Attachment)(nil)).
		Set("state = ?", tenant.AttachmentStateDeleted).
		Set("deleted_at = ?", now).
		Set(`deleter_identifier = ?`, user.Identifier).
		Where("identifier = ?", att.Identifier).
		Exec(ctx)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("update attachment: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{
		"message": fmt.Sprintf("Attachment %s deleted successfully from CFDI %s", fileName, cfdiUUID),
	})
}
