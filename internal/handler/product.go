package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/db"
	"github.com/siigofiscal/go_backend/internal/model/control"
	"github.com/siigofiscal/go_backend/internal/response"
)

type Product struct {
	cfg      *config.Config
	database *db.Database
}

func NewProduct(cfg *config.Config, database *db.Database) *Product {
	return &Product{cfg: cfg, database: database}
}

// Set handles POST /api/Product/set — Stripe webhook that upserts a product record.
//
// Python source: ProductStripeConstructor.construct() verifies stripe-signature via
// stripe.Webhook.construct_event(), then saves the product via ProductRepositorySA.
// Full Stripe SDK verification (github.com/stripe/stripe-go/v81) is added in Phase 10.
// This phase accepts the raw JSON body and upserts directly so the endpoint is functional.
func (h *Product) Set(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.BadRequest(w, "cannot read body")
		return
	}

	var event struct {
		Type string `json:"type"`
		Data struct {
			Object struct {
				ID           string            `json:"id"`
				Name         string            `json:"name"`
				DefaultPrice string            `json:"default_price"`
				Metadata     map[string]string `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		response.BadRequest(w, fmt.Sprintf("invalid JSON: %v", err))
		return
	}

	if event.Type != "product.created" && event.Type != "product.updated" {
		// Acknowledge unhandled event types without error (Stripe expects 2xx).
		response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	obj := event.Data.Object
	if obj.ID == "" {
		response.BadRequest(w, "missing product id in event")
		return
	}

	metaJSON, _ := json.Marshal(obj.Metadata)

	product := &control.Product{
		StripeIdentifier:      obj.ID,
		StripeName:            obj.Name,
		StripePriceIdentifier: obj.DefaultPrice,
		Characteristics:       metaJSON,
		Price:                 0, // price.unit_amount requires Stripe API call (Phase 10)
	}

	_, err = h.database.Primary.NewInsert().
		Model(product).
		On("CONFLICT (stripe_identifier) DO UPDATE").
		Set("stripe_name = EXCLUDED.stripe_name").
		Set("stripe_price_identifier = EXCLUDED.stripe_price_identifier").
		Set("characteristics = EXCLUDED.characteristics").
		Exec(r.Context())
	if err != nil {
		response.InternalError(w, fmt.Sprintf("upsert product: %v", err))
		return
	}

	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GetAll handles GET /api/Product/get_all — returns active products.
//
// Python source: get_list_of_products(0) calls Stripe API (or mock) and returns
// {products: [...]}. In mock mode we return DB products; Stripe live call added in Phase 10.
func (h *Product) GetAll(w http.ResponseWriter, r *http.Request) {
	accessToken := r.Header.Get("access_token")
	if accessToken == "" {
		response.Unauthorized(w, "")
		return
	}

	if h.cfg.MockStripe {
		response.WriteJSON(w, http.StatusOK, mockProducts())
		return
	}

	var products []control.Product
	if err := h.database.Replica.NewSelect().
		Model(&products).
		OrderExpr("price ASC").
		Scan(r.Context()); err != nil {
		response.InternalError(w, fmt.Sprintf("query products: %v", err))
		return
	}

	items := make([]map[string]interface{}, 0, len(products))
	for _, p := range products {
		items = append(items, map[string]interface{}{
			"identifier":              p.StripeIdentifier,
			"execute_at":              nil,
			"stripe_identifier":       p.StripeIdentifier,
			"characteristics":         p.Characteristics,
			"price":                   p.Price,
			"stripe_price_identifier": p.StripePriceIdentifier,
			"stripe_name":             p.StripeName,
		})
	}

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{"products": items})
}

func mockProducts() map[string]interface{} {
	return map[string]interface{}{
		"products": []map[string]interface{}{
			{
				"identifier":              "prod_MjDE9ihnCFzJn7",
				"execute_at":              nil,
				"stripe_identifier":       "prod_MjDE9ihnCFzJn7",
				"characteristics":         map[string]string{"max_companies": "1", "max_emails_enroll": "1", "add_enabled": "false", "exceed_metadata_limit": "false"},
				"price":                   99900,
				"stripe_price_identifier": "price_1MockBasicPrice",
				"stripe_name":             "Plan Básico - Local Dev",
			},
			{
				"identifier":              "prod_MockProfessional",
				"execute_at":              nil,
				"stripe_identifier":       "prod_MockProfessional",
				"characteristics":         map[string]string{"max_companies": "5", "max_emails_enroll": "5", "add_enabled": "true", "exceed_metadata_limit": "false"},
				"price":                   199900,
				"stripe_price_identifier": "price_2MockProfessionalPrice",
				"stripe_name":             "Plan Profesional - Local Dev",
			},
			{
				"identifier":              "prod_MockEnterprise",
				"execute_at":              nil,
				"stripe_identifier":       "prod_MockEnterprise",
				"characteristics":         map[string]string{"max_companies": "999", "max_emails_enroll": "999", "add_enabled": "true", "exceed_metadata_limit": "true"},
				"price":                   499900,
				"stripe_price_identifier": "price_3MockEnterprisePrice",
				"stripe_name":             "Plan Empresarial - Local Dev",
			},
		},
	}
}
