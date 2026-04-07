package server

import (
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	"github.com/siigofiscal/go_backend/internal/domain/event"
	"github.com/siigofiscal/go_backend/internal/domain/port"
	"github.com/siigofiscal/go_backend/internal/handler"
	stripeinfra "github.com/siigofiscal/go_backend/internal/infra/stripe"
	"github.com/siigofiscal/go_backend/internal/server/middleware"
)

func New(cfg *config.Config, database *db.Database, bus *event.Bus, files port.FileStorage, idp port.IdentityProvider, jwtDecoder *auth.JWTDecoder, stripeClient ...*stripeinfra.Client) http.Handler {
	var sc *stripeinfra.Client
	if len(stripeClient) > 0 {
		sc = stripeClient[0]
	} else {
		sc = stripeinfra.NewClient(cfg)
	}
	mux := http.NewServeMux()
	authMW := middleware.NewAuth(cfg, database, jwtDecoder)

	// --- Status / Health (no auth) ---
	statusHandler := handler.NewStatus(cfg, database, bus)
	mux.HandleFunc("GET /api/status/health/api", statusHandler.HealthAPI)
	mux.HandleFunc("GET /api/status/health/db", statusHandler.HealthDB)
	mux.HandleFunc("GET /api/status/version", statusHandler.Version)
	mux.HandleFunc("POST /api/status/sqs-test", statusHandler.SQSTest)

	// --- Dev auth (LOCAL_INFRA only) ---
	if cfg.LocalInfra {
		devAuth := handler.NewDevAuth(cfg, database)
		mux.HandleFunc("POST /api/dev/login", devAuth.Login)
		mux.HandleFunc("POST /api/dev/token", devAuth.GenerateToken)
		mux.HandleFunc("GET /api/dev/users", devAuth.ListUsers)
		mux.HandleFunc("GET /api/dev/auth-status", devAuth.AuthStatus)
	}

	// --- Phase 7: Company management (11 endpoints) ---
	companyH := handler.NewCompany(cfg, database, bus, files)
	mux.HandleFunc("POST /api/Company/upload_cer", authMW.RequireCompany(companyH.UploadCer))
	mux.HandleFunc("POST /api/Company/get_cer", authMW.RequireCompany(companyH.GetCer))
	mux.HandleFunc("POST /api/Company/search", companyH.Search)
	mux.HandleFunc("POST /api/Company/", authMW.RequireAuth(companyH.Create))
	mux.HandleFunc("POST /api/Company/admin_create", authMW.RequireAdminCreate(companyH.AdminCreate))
	mux.HandleFunc("PUT /api/Company/", authMW.RequireAuth(companyH.Update))
	mux.HandleFunc("DELETE /api/Company/", authMW.RequireAuth(companyH.Delete))
	mux.HandleFunc("GET /api/Company/{cid}/data/{key}", authMW.RequireAuth(companyH.GetData))
	mux.HandleFunc("PUT /api/Company/{cid}/data/{key}", authMW.RequireAuth(companyH.SetData))
	mux.HandleFunc("PUT /api/Company/set_isr_percentage", authMW.RequireCompany(companyH.SetISRPercentage))
	mux.HandleFunc("GET /api/Company/get_isr_percentage", authMW.RequireCompany(companyH.GetISRPercentage))

	// --- Phase 7: User management (15 endpoints) ---
	userH := handler.NewUser(cfg, database, idp, jwtDecoder)
	mux.HandleFunc("POST /api/User/auth", userH.Auth)
	mux.HandleFunc("GET /api/User/auth/{code}", userH.AuthByCode)
	mux.HandleFunc("POST /api/User/auth_challenge", userH.AuthChallenge)
	mux.HandleFunc("POST /api/User/", userH.CreateUser)
	mux.HandleFunc("GET /api/User/", authMW.RequireAuth(userH.GetUser))
	mux.HandleFunc("PUT /api/User/", authMW.RequireAuth(userH.UpdateUser))
	mux.HandleFunc("POST /api/User/change_password", userH.ChangePassword)
	mux.HandleFunc("POST /api/User/forgot", userH.Forgot)
	mux.HandleFunc("POST /api/User/confirm_forgot", userH.ConfirmForgot)
	mux.HandleFunc("POST /api/User/config", authMW.RequireCompany(userH.PostConfig))
	mux.HandleFunc("GET /api/User/config/{company_identifier}", authMW.RequireAuth(userH.GetConfig))
	mux.HandleFunc("POST /api/User/super_invite", authMW.RequireAuth(userH.SuperInvite))
	mux.HandleFunc("PUT /api/User/set_email/{old_email}/{new_email}", authMW.RequireAdmin(userH.SetEmail))
	mux.HandleFunc("POST /api/User/update_fiscal_data", authMW.RequireAuth(userH.UpdateFiscalData))
	mux.HandleFunc("GET /api/User/update_fiscal_data", authMW.RequireAuth(userH.GetFiscalData))

	// --- Phase 8: Tenant CRUD & Attachments ---

	// Poliza — 3 endpoints
	polizaH := handler.NewPoliza(cfg, database, files)
	mux.HandleFunc("POST /api/Poliza/search", authMW.RequireCompany(polizaH.Search))
	mux.HandleFunc("POST /api/Poliza/create_many", authMW.RequireCompany(polizaH.CreateMany))
	mux.HandleFunc("POST /api/Poliza/export", authMW.RequireCompany(polizaH.Export))

	// DoctoRelacionado — 3 endpoints
	doctoH := handler.NewDoctoRelacionado(cfg, database, files)
	mux.HandleFunc("POST /api/DoctoRelacionado/search", authMW.RequireCompany(doctoH.Search))
	mux.HandleFunc("POST /api/DoctoRelacionado/update", authMW.RequireCompany(doctoH.Update))
	mux.HandleFunc("POST /api/DoctoRelacionado/export_isr_pagos", authMW.RequireCompany(doctoH.ExportISRPagos))

	// Attachment — 4 endpoints
	attachmentH := handler.NewAttachment(cfg, database, files)
	mux.HandleFunc("POST /api/Attachment/search", authMW.RequireCompany(attachmentH.Search))
	mux.HandleFunc("POST /api/Attachment/{company_identifier}/{uuid}", authMW.RequireAuth(attachmentH.CreateMany))
	mux.HandleFunc("GET /api/Attachment/{company_identifier}/{uuid}", authMW.RequireAuth(attachmentH.GetDownloadURLs))
	mux.HandleFunc("DELETE /api/Attachment/{company_identifier}/{uuid}/{file_name...}", authMW.RequireAuth(attachmentH.DeleteAttachment))

	// EFOS — 3 endpoints
	efosH := handler.NewEFOS(cfg, database)
	mux.HandleFunc("POST /api/EFOS/update", authMW.RequireAuth(efosH.UpdateFromSAT))
	mux.HandleFunc("POST /api/EFOS/search", authMW.RequireCompany(efosH.Search))
	mux.HandleFunc("POST /api/EFOS/resume", authMW.RequireCompany(efosH.Resume))

	// CfdiExport — 1 endpoint
	cfdiExportH := handler.NewCfdiExport(cfg, database)
	mux.HandleFunc("POST /api/Export/search", authMW.RequireCompany(cfdiExportH.Search))

	// CfdiExcluded — 1 endpoint
	cfdiExcludedH := handler.NewCfdiExcluded(cfg, database)
	mux.HandleFunc("POST /api/CFDIExcluded/search", authMW.RequireCompany(cfdiExcludedH.Search))

	// --- Phase 9: CFDI Core (22 endpoints) ---
	cfdiH := handler.NewCFDI(cfg, database, bus, files)
	mux.HandleFunc("POST /api/CFDI/search", authMW.RequireCompany(cfdiH.Search))
	mux.HandleFunc("POST /api/CFDI/export", authMW.RequireCompany(cfdiH.Export))
	mux.HandleFunc("POST /api/CFDI/massive_export", authMW.RequireCompany(cfdiH.MassiveExport))
	mux.HandleFunc("POST /api/CFDI/export_iva", authMW.RequireCompany(cfdiH.ExportIVA))
	mux.HandleFunc("POST /api/CFDI/get_export_cfdi", authMW.RequireCompany(cfdiH.GetExportCFDI))
	mux.HandleFunc("POST /api/CFDI/get_exports", authMW.RequireCompany(cfdiH.GetExports))
	mux.HandleFunc("POST /api/CFDI/get_xml", authMW.RequireCompany(cfdiH.GetXML))
	mux.HandleFunc("POST /api/CFDI/get_by_period", authMW.RequireCompany(cfdiH.GetByPeriod))
	mux.HandleFunc("POST /api/CFDI/resume", authMW.RequireCompany(cfdiH.Resume))
	mux.HandleFunc("POST /api/CFDI/get_count_cfdis", authMW.RequireCompany(cfdiH.GetCountCfdis))
	mux.HandleFunc("POST /api/CFDI/get_iva", authMW.RequireCompany(cfdiH.GetIVA))
	mux.HandleFunc("POST /api/CFDI/get_iva_all", authMW.RequireCompany(cfdiH.GetIVAAll))
	mux.HandleFunc("POST /api/CFDI/get_isr", authMW.RequireCompany(cfdiH.GetISR))
	mux.HandleFunc("POST /api/CFDI/search_iva", authMW.RequireCompany(cfdiH.SearchIVA))
	mux.HandleFunc("POST /api/CFDI/update", authMW.RequireCompany(cfdiH.Update))
	mux.HandleFunc("POST /api/CFDI/export_isr", authMW.RequireCompany(cfdiH.ExportISR))
	mux.HandleFunc("POST /api/CFDI/total_deducciones_cfdi", authMW.RequireCompany(cfdiH.TotalDeduccionesCFDI))
	mux.HandleFunc("POST /api/CFDI/total_deducciones_pagos", authMW.RequireCompany(cfdiH.TotalDeduccionesPagos))
	mux.HandleFunc("POST /api/CFDI/totales", authMW.RequireCompany(cfdiH.Totales))
	mux.HandleFunc("POST /api/CFDI/export_isr_totales", authMW.RequireCompany(cfdiH.ExportISRTotales))
	mux.HandleFunc("POST /api/CFDI/export_isr_cfdi", authMW.RequireCompany(cfdiH.ExportISRCFDI))
	mux.HandleFunc("GET /api/CFDI/{cid}/emitidos/ingresos/{anio}/{mes}/resumen", authMW.RequireCompany(cfdiH.EmitidosIngresosResumen))

	// --- Phase 10: SAT Query (5 endpoints) ---
	satQueryH := handler.NewSATQuery(cfg, database, bus)
	mux.HandleFunc("POST /api/SATQuery/search", authMW.RequireCompany(satQueryH.Search))
	mux.HandleFunc("POST /api/SATQuery/manual", authMW.RequireCompany(satQueryH.Manual))
	mux.HandleFunc("POST /api/SATQuery/can_manual_request", authMW.RequireCompany(satQueryH.CanManualRequest))
	mux.HandleFunc("POST /api/SATQuery/log", authMW.RequireCompany(satQueryH.Log))
	mux.HandleFunc("POST /api/SATQuery/massive_scrap", authMW.RequireAuth(satQueryH.MassiveScrap))

	// --- Phase 10: Scraper (2 endpoints) ---
	scraperH := handler.NewScraper(cfg, database, bus, files)
	mux.HandleFunc("POST /api/Scraper/scrap_sat_pdf", authMW.RequireCompany(scraperH.ScrapSatPDF))
	mux.HandleFunc("POST /api/Scraper/get_pdf_files", authMW.RequireAuth(scraperH.GetPDFFiles))

	// --- Phase 10: License (6 endpoints) ---
	licenseH := handler.NewLicense(cfg, database, sc)
	mux.HandleFunc("PUT /api/License/", authMW.RequireAuth(licenseH.Put))
	mux.HandleFunc("POST /api/License/paid/alert", licenseH.PaidAlert)
	mux.HandleFunc("PUT /api/License/source", authMW.RequireAuth(licenseH.SetSource))
	mux.HandleFunc("POST /api/License/", authMW.RequireAuth(licenseH.GetLicenseDetails))
	mux.HandleFunc("POST /api/License/set", authMW.RequireAuth(licenseH.SetLicense))
	mux.HandleFunc("GET /api/License/{workspace_identifier}", licenseH.GetFreeTrialByWorkspace)

	// --- Phase 11: Pasto/ADD (13 endpoints) ---

	// Pasto Config — 1 endpoint (ADD_CONFIG_WEBHOOK = "Pasto/Config")
	pastoConfigH := handler.NewPastoConfig(cfg, database, bus)
	mux.HandleFunc("POST /api/"+cfg.ADDConfigWebhook, pastoConfigH.ConfigWebhook)

	// Pasto Sync — 4 endpoints
	pastoSyncH := handler.NewPastoSync(cfg, database, bus)
	mux.HandleFunc("POST /api/Pasto/Sync/", authMW.RequireCompany(pastoSyncH.CreateSyncRequest))
	mux.HandleFunc("POST /api/Pasto/Sync/enable_auto_sync", authMW.RequireAuth(pastoSyncH.EnableAutoSync))
	mux.HandleFunc("POST /api/Pasto/Sync/create_metadata_sync_request", authMW.RequireAuth(pastoSyncH.CreateMetadataSyncRequest))
	mux.HandleFunc("POST /api/Pasto/Sync/search", authMW.RequireCompany(pastoSyncH.Search))

	// Pasto Metadata webhook — 1 endpoint (ADD_METADATA_WEBHOOK = "Pasto/Metadata")
	pastoMetaH := handler.NewPastoMetadata(cfg, database, bus)
	mux.HandleFunc("POST /api/"+cfg.ADDMetadataWebhook, pastoMetaH.MetadataWebhook)

	// Pasto XML webhook — 1 endpoint (ADD_XML_WEBHOOK = "Pasto/XML")
	pastoXMLH := handler.NewPastoXML(cfg, database)
	mux.HandleFunc("POST /api/"+cfg.ADDXMLWebhook, pastoXMLH.XMLWebhook)

	// Pasto Cancel webhook — 1 endpoint (ADD_CANCEL_WEBHOOK = "Pasto/Cancel")
	pastoCancelH := handler.NewPastoCancel(cfg, database)
	mux.HandleFunc("POST /api/"+cfg.ADDCancelWebhook, pastoCancelH.CancelWebhook)

	// Pasto ResetLicense — 1 endpoint
	pastoResetH := handler.NewPastoReset(cfg, database, bus)
	mux.HandleFunc("POST /api/Pasto/ResetLicense/", authMW.RequireAuth(pastoResetH.ResetLicense))

	// Pasto Worker — 1 endpoint
	pastoWorkerH := handler.NewPastoWorker(cfg, database, bus)
	mux.HandleFunc("POST /api/Pasto/Worker/", authMW.RequireAuth(pastoWorkerH.CreateWorker))

	// Pasto Company — 3 endpoints
	pastoCompanyH := handler.NewPastoCompany(cfg, database)
	mux.HandleFunc("POST /api/"+cfg.ADDCompaniesWebhook, pastoCompanyH.CompanyWebhook)
	mux.HandleFunc("POST /api/Pasto/Company/search", authMW.RequireAuth(pastoCompanyH.Search))
	mux.HandleFunc("POST /api/Pasto/Company/request_new", authMW.RequireAuth(pastoCompanyH.RequestNew))

	// --- Phase 11: COI (4 endpoints) ---
	coiH := handler.NewCOI(cfg, database, bus, files)
	mux.HandleFunc("POST /api/COI/search", authMW.RequireCompany(coiH.Search))
	mux.HandleFunc("GET /api/COI/{company_identifier}/{identifier}", authMW.RequireCompany(coiH.Get))
	mux.HandleFunc("POST /api/COI/{company_identifier}/{identifier}/notify", authMW.RequireCompany(coiH.NotifyMetadataUploaded))
	mux.HandleFunc("POST /api/COI/{company_identifier}", authMW.RequireCompany(coiH.NewSync))

	// --- Phase 5: Simple & CRUD routes ---

	// Param — 1 endpoint (no auth)
	paramH := handler.NewParam(cfg, database)
	mux.HandleFunc("POST /api/Param/search", paramH.Search)

	// RegimenFiscal — 1 endpoint (no auth)
	regimenH := handler.NewRegimenFiscal(cfg, database)
	mux.HandleFunc("GET /api/RegimenFiscal/", regimenH.GetAll)

	// Product — 2 endpoints
	productH := handler.NewProduct(cfg, database)
	mux.HandleFunc("POST /api/Product/set", productH.Set)
	mux.HandleFunc("GET /api/Product/get_all", productH.GetAll)

	// Permission — 2 endpoints
	permissionH := handler.NewPermission(cfg, database)
	mux.HandleFunc("POST /api/Permission/search", permissionH.Search)
	mux.HandleFunc("PUT /api/Permission/", authMW.RequireAuth(permissionH.SetPermissions))

	// Notification — 2 endpoints
	notificationH := handler.NewNotification(cfg, database)
	mux.HandleFunc("POST /api/Notification/config/search", notificationH.Search)
	mux.HandleFunc("PUT /api/Notification/config", authMW.RequireAuth(notificationH.SetConfig))

	// Workspace — 6 endpoints
	workspaceH := handler.NewWorkspace(cfg, database)
	mux.HandleFunc("POST /api/Workspace/search", workspaceH.Search)
	mux.HandleFunc("POST /api/Workspace/", authMW.RequireAuth(workspaceH.Create))
	mux.HandleFunc("PUT /api/Workspace/", authMW.RequireAuth(workspaceH.Update))
	mux.HandleFunc("DELETE /api/Workspace/", authMW.RequireAuth(workspaceH.Delete))
	mux.HandleFunc("GET /api/Workspace/{workspace_id}/license/{key}", authMW.RequireAdminCreate(workspaceH.GetLicense))
	mux.HandleFunc("PUT /api/Workspace/{workspace_id}/license/{key}", authMW.RequireAdminCreate(workspaceH.SetLicense))

	// --- Global middleware chain ---
	var h http.Handler = mux
	h = db.InjectDatabase(database)(h)
	h = middleware.Recovery(h)
	h = middleware.Logging(h)
	if cfg.LocalInfra {
		h = middleware.CORS(h)
	}

	return h
}
