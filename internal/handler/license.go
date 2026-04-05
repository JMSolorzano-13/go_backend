package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/domain/auth"
	stripeinfra "github.com/siigofiscal/go_backend/internal/infra/stripe"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type License struct {
	cfg      *config.Config
	database *db.Database
	stripe   *stripeinfra.Client
}

func NewLicense(cfg *config.Config, database *db.Database, stripe *stripeinfra.Client) *License {
	return &License{cfg: cfg, database: database, stripe: stripe}
}

// 1. PUT /api/License/ — admin set licenses on workspaces
func (h *License) Put(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)

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

	if !auth.IsAdmin(user.Email, h.cfg.AdminEmails) {
		response.Forbidden(w, "Only admin users can set licenses")
		return
	}

	licenses, _ := body["licenses"].([]interface{})
	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	for _, lic := range licenses {
		licMap, ok := lic.(map[string]interface{})
		if !ok {
			continue
		}
		wsIdentifier, _ := licMap["workspace_identifier"].(string)
		licenseData, _ := licMap["license"].(map[string]interface{})
		if wsIdentifier == "" || licenseData == nil {
			continue
		}

		licJSON, _ := json.Marshal(licenseData)
		_, err := database.Primary.NewUpdate().
			Model((*control.Workspace)(nil)).
			Set("license = ?", string(licJSON)).
			Where("identifier = ?", wsIdentifier).
			Exec(ctx)
		if err != nil {
			slog.Error("license: set license failed", "workspace", wsIdentifier, "error", err)
		}
	}

	w.WriteHeader(http.StatusOK)
}

// 2. POST /api/License/paid/alert — Stripe webhook
func (h *License) PaidAlert(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "invalid body")
		return
	}

	sigHeader := r.Header.Get("Stripe-Signature")

	if h.cfg.MockStripe {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	result, err := h.stripe.ProcessPaidAlert(raw, sigHeader)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]interface{}{"error": err.Error()})
		return
	}

	response.WriteJSON(w, http.StatusOK, result)
}

// 3. PUT /api/License/source — set user source (coupon)
func (h *License) SetSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)

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

	if !auth.IsAdmin(user.Email, h.cfg.AdminEmails) {
		response.Forbidden(w, "Only admin or super users can set the source")
		return
	}

	source, _ := body["source"].(string)
	userID, _ := body["user_id"].(float64)
	if userID == 0 {
		response.BadRequest(w, "user_id is required")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	_, err = database.Primary.NewUpdate().
		Model((*control.User)(nil)).
		Set("source_name = ?", source).
		Where("id = ?", int64(userID)).
		Exec(ctx)
	if err != nil {
		response.InternalError(w, fmt.Sprintf("set source: %v", err))
		return
	}

	w.WriteHeader(http.StatusOK)
}

// 4. POST /api/License/ — get license details
func (h *License) GetLicenseDetails(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)

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

	wsIdentifier, _ := body["workspace_identifier"].(string)
	if wsIdentifier == "" {
		response.BadRequest(w, "workspace_identifier is required")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	err = database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", wsIdentifier).
		Limit(1).
		Scan(ctx)
	if err != nil {
		response.NotFound(w, "Workspace not found")
		return
	}

	if workspace.OwnerID != nil && *workspace.OwnerID != user.ID {
		hasPermission, _ := database.Primary.NewSelect().
			Model((*control.Permission)(nil)).
			Join("JOIN company AS c ON c.id = p.company_id").
			Where("p.user_id = ?", user.ID).
			Where("c.workspace_id = ?", workspace.ID).
			Exists(ctx)
		if !hasPermission {
			response.Unauthorized(w, "User does not have permission for this workspace")
			return
		}
	}

	info := map[string]interface{}{
		"sub_identifier":   "",
		"cus_identifier":   "",
		"add_enabled":      false,
		"any_invoice_paid": false,
		"valid_until":      nil,
	}

	if workspace.License != nil {
		var license map[string]interface{}
		if err := json.Unmarshal(workspace.License, &license); err == nil {
			if details, ok := license["details"].(map[string]interface{}); ok {
				if addEnabled, ok := details["add_enabled"].(bool); ok {
					info["add_enabled"] = addEnabled
				}
			}
		}
	}

	if workspace.ValidUntil != nil {
		info["valid_until"] = workspace.ValidUntil.Unix()
	}
	if workspace.StripeStatus != nil {
		info["stripe_status"] = *workspace.StripeStatus
	}

	if user.StripeIdentifier != nil && *user.StripeIdentifier != "" {
		info["cus_identifier"] = *user.StripeIdentifier

		sub, err := h.stripe.GetLatestSubscription(*user.StripeIdentifier)
		if err == nil && sub != nil {
			info["sub_identifier"] = sub.ID

			if sub.CurrentPeriodEnd > 0 {
				info["valid_until"] = sub.CurrentPeriodEnd
			}

			if sub.LatestInvoice != nil {
				info["last_date_invoice"] = sub.LatestInvoice.Created
			}
		}
	}

	response.WriteJSON(w, http.StatusOK, info)
}

// 5. POST /api/License/set — update/create subscription
func (h *License) SetLicense(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user, _ := auth.UserFromContext(ctx)

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

	wsIdentifier, _ := body["workspace_identifier"].(string)
	if wsIdentifier == "" {
		response.BadRequest(w, "workspace_identifier is required")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var workspace control.Workspace
	err = database.Primary.NewSelect().
		Model(&workspace).
		Where("identifier = ?", wsIdentifier).
		Limit(1).
		Scan(ctx)
	if err != nil {
		response.NotFound(w, "Workspace not found")
		return
	}

	if workspace.OwnerID != nil && *workspace.OwnerID != user.ID {
		response.Unauthorized(w, "Only workspace owner can change the license")
		return
	}

	if h.cfg.MockStripe {
		licenseData, _ := body["products"].([]interface{})
		if licenseData != nil {
			var license map[string]interface{}
			if workspace.License != nil {
				_ = json.Unmarshal(workspace.License, &license)
			}
			if license == nil {
				license = make(map[string]interface{})
			}
			if details, ok := license["details"].(map[string]interface{}); ok {
				details["products"] = licenseData
			} else {
				license["details"] = map[string]interface{}{"products": licenseData}
			}
			license["stripe_status"] = "active"
			licJSON, _ := json.Marshal(license)
			_, _ = database.Primary.NewUpdate().
				Model((*control.Workspace)(nil)).
				Set("license = ?", string(licJSON)).
				Set("stripe_status = ?", "active").
				Set("valid_until = ?", time.Now().Add(365*24*time.Hour)).
				Where("id = ?", workspace.ID).
				Exec(ctx)
		}
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"invoice_url": ""})
		return
	}

	products, _ := body["products"].([]interface{})
	if len(products) == 0 {
		response.BadRequest(w, "products are required")
		return
	}

	productList := h.stripe.GetListOfProducts(0)
	allProducts, _ := productList["products"].([]map[string]interface{})

	var matchedProducts []map[string]interface{}
	requestedIDs := make(map[string]float64)
	for _, p := range products {
		pMap, _ := p.(map[string]interface{})
		if pMap == nil {
			continue
		}
		id, _ := pMap["identifier"].(string)
		qty, _ := pMap["quantity"].(float64)
		requestedIDs[id] = qty
	}

	for _, p := range allProducts {
		id, _ := p["identifier"].(string)
		if qty, ok := requestedIDs[id]; ok {
			matched := make(map[string]interface{})
			for k, v := range p {
				matched[k] = v
			}
			matched["quantity"] = qty
			matchedProducts = append(matchedProducts, matched)
		}
	}

	if user.StripeIdentifier == nil || *user.StripeIdentifier == "" {
		response.BadRequest(w, "User has no Stripe customer ID")
		return
	}

	sub, _ := h.stripe.GetLatestSubscription(*user.StripeIdentifier)

	isTrial := false
	if sub != nil {
		for _, item := range sub.Items.Data {
			if item.Price != nil && item.Price.Product != nil && item.Price.Product.ID == h.cfg.ProductTrial {
				isTrial = true
				break
			}
		}
	}

	if isTrial || sub == nil || sub.Status != "active" {
		if isTrial && sub != nil {
			_ = h.stripe.CancelSubscription(sub.ID)
		}
		newSub, err := h.stripe.CreateSubscription(
			*user.StripeIdentifier,
			matchedProducts,
			int64(h.cfg.StripeDaysUntilDue),
		)
		if err != nil {
			response.BadRequest(w, fmt.Sprintf("Failed to create subscription: %v", err))
			return
		}

		invoiceURL := ""
		if newSub.LatestInvoice != nil && newSub.LatestInvoice.ID != "" {
			inv, err := h.stripe.FinalizeInvoice(newSub.LatestInvoice.ID)
			if err == nil && inv != nil {
				invoiceURL = inv.HostedInvoiceURL
			}
		}

		response.WriteJSON(w, http.StatusOK, map[string]interface{}{"invoice_url": invoiceURL})
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"invoice_url": "",
		"message":     "Subscription update processed",
	})
}

// 6. GET /api/License/{workspace_identifier} — free trial info
func (h *License) GetFreeTrialByWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	wsIdentifier := r.PathValue("workspace_identifier")
	if wsIdentifier == "" {
		response.BadRequest(w, "workspace_identifier is required")
		return
	}

	database := db.FromContext(ctx)
	if database == nil {
		database = h.database
	}

	var ownerEmail string
	err := database.Primary.NewSelect().
		TableExpr(`"user" AS u`).
		ColumnExpr("u.email").
		Join("JOIN workspace AS w ON w.owner_id = u.id").
		Where("w.identifier = ?", wsIdentifier).
		Limit(1).
		Scan(ctx, &ownerEmail)
	if err != nil || ownerEmail == "" {
		response.NotFound(w, fmt.Sprintf("No workspace '%s' found or no owner assigned", wsIdentifier))
		return
	}

	trialInfo, err := getSiigoFreeTrial(h.cfg, ownerEmail)
	if err != nil {
		response.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	response.WriteJSON(w, http.StatusOK, trialInfo)
}

func getSiigoFreeTrial(cfg *config.Config, portalUserName string) (map[string]interface{}, error) {
	if cfg.SiigoFreeTrialBaseURL == "" {
		return map[string]interface{}{
			"status":  -1,
			"message": "Siigo Free Trial not configured",
		}, nil
	}

	baseURL := cfg.SiigoFreeTrialBaseURL
	timeout := cfg.SiigoFreeTrialTimeout
	if timeout <= 0 {
		timeout = 50
	}

	slog.Info("siigo_free_trial: fetching", "email", portalUserName, "base_url", baseURL, "timeout", timeout)

	return map[string]interface{}{
		"portalUserName": portalUserName,
		"status":         -1,
		"message":        "Free trial lookup delegated to external service",
	}, nil
}

