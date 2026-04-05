package stripe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	stripeAPI "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/invoice"
	"github.com/stripe/stripe-go/v81/product"
	"github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/subscriptionitem"
	"github.com/stripe/stripe-go/v81/webhook"

	"github.com/siigofiscal/go_backend/internal/config"
)

type Client struct {
	cfg  *config.Config
	mock bool
}

func NewClient(cfg *config.Config) *Client {
	if cfg.StripeSecretKey != "" {
		stripeAPI.Key = cfg.StripeSecretKey
	}
	return &Client{cfg: cfg, mock: cfg.MockStripe}
}

func (c *Client) VerifyWebhookSignature(payload []byte, sigHeader string) (*stripeAPI.Event, error) {
	event, err := webhook.ConstructEvent(payload, sigHeader, c.cfg.StripeWebhookPaidAlert)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (c *Client) GetLatestSubscription(customerID string) (*stripeAPI.Subscription, error) {
	if c.mock || customerID == "" {
		return nil, nil
	}

	var all []*stripeAPI.Subscription
	for _, status := range []string{"active", "past_due", "canceled"} {
		params := &stripeAPI.SubscriptionListParams{
			Customer: stripeAPI.String(customerID),
			Status:   stripeAPI.String(status),
		}
		iter := subscription.List(params)
		for iter.Next() {
			all = append(all, iter.Subscription())
		}
	}

	if len(all) == 0 {
		return nil, nil
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Created > all[j].Created
	})
	return all[0], nil
}

func (c *Client) GetListOfProducts(isRenew int) map[string]interface{} {
	if c.mock {
		return getMockProducts(isRenew)
	}

	params := &stripeAPI.ProductListParams{Active: stripeAPI.Bool(true)}
	iter := product.List(params)

	var products []map[string]interface{}
	for iter.Next() {
		p := iter.Product()

		priceData := map[string]interface{}{
			"identifier":              p.ID,
			"stripe_price_identifier": p.DefaultPrice.ID,
			"stripe_name":             p.Name,
		}

		if p.Metadata != nil {
			for k, v := range p.Metadata {
				priceData[k] = v
			}
		}

		products = append(products, priceData)
	}

	return map[string]interface{}{
		"products": products,
	}
}

func (c *Client) CancelSubscription(subID string) error {
	if c.mock {
		return nil
	}
	_, err := subscription.Cancel(subID, nil)
	return err
}

func (c *Client) ModifySubscription(subID string, items []*stripeAPI.SubscriptionItemsParams) error {
	if c.mock {
		return nil
	}
	_, err := subscription.Update(subID, &stripeAPI.SubscriptionParams{
		Items:             items,
		ProrationBehavior: stripeAPI.String("none"),
	})
	return err
}

func (c *Client) CreateSubscription(customerID string, items []map[string]interface{}, daysUntilDue int64) (*stripeAPI.Subscription, error) {
	if c.mock {
		return &stripeAPI.Subscription{ID: "mock_sub_" + customerID}, nil
	}

	var subItems []*stripeAPI.SubscriptionItemsParams
	for _, item := range items {
		priceID, _ := item["stripe_price_identifier"].(string)
		qty := int64(1)
		if q, ok := item["quantity"].(float64); ok {
			qty = int64(q)
		}
		subItems = append(subItems, &stripeAPI.SubscriptionItemsParams{
			Price:    stripeAPI.String(priceID),
			Quantity: stripeAPI.Int64(qty),
		})
	}

	params := &stripeAPI.SubscriptionParams{
		Customer:          stripeAPI.String(customerID),
		Items:             subItems,
		DaysUntilDue:      stripeAPI.Int64(daysUntilDue),
		CollectionMethod:  stripeAPI.String("send_invoice"),
		PaymentSettings:   &stripeAPI.SubscriptionPaymentSettingsParams{},
	}

	return subscription.New(params)
}

func (c *Client) ListSubscriptionItems(subID string) ([]*stripeAPI.SubscriptionItem, error) {
	if c.mock {
		return nil, nil
	}
	params := &stripeAPI.SubscriptionItemListParams{
		Subscription: stripeAPI.String(subID),
	}
	iter := subscriptionitem.List(params)
	var items []*stripeAPI.SubscriptionItem
	for iter.Next() {
		items = append(items, iter.SubscriptionItem())
	}
	return items, iter.Err()
}

func (c *Client) GetCustomer(customerID string) (*stripeAPI.Customer, error) {
	if c.mock {
		return &stripeAPI.Customer{ID: customerID}, nil
	}
	return customer.Get(customerID, nil)
}

func (c *Client) FinalizeInvoice(invoiceID string) (*stripeAPI.Invoice, error) {
	if c.mock {
		return &stripeAPI.Invoice{ID: invoiceID}, nil
	}
	return invoice.FinalizeInvoice(invoiceID, nil)
}

func (c *Client) ProcessPaidAlert(payload []byte, sigHeader string) (map[string]interface{}, error) {
	ev, err := c.VerifyWebhookSignature(payload, sigHeader)
	if err != nil {
		return nil, fmt.Errorf("invalid signature")
	}

	if ev.Type != "invoice.paid" {
		return map[string]interface{}{"success": true}, nil
	}

	var invoiceObj map[string]interface{}
	if err := json.Unmarshal(ev.Data.Raw, &invoiceObj); err != nil {
		return map[string]interface{}{"success": true}, nil
	}

	subID, _ := invoiceObj["subscription"].(string)
	if subID == "" {
		return map[string]interface{}{"success": true}, nil
	}

	sub, err := c.GetLatestSubscription("")
	if err != nil || sub == nil {
		slog.Error("stripe: failed to retrieve subscription for paid alert", "sub_id", subID)
		return map[string]interface{}{"success": true}, nil
	}

	productsRenew := c.GetListOfProducts(1)
	renewalMap := make(map[string]string)
	if prods, ok := productsRenew["products"].([]map[string]interface{}); ok {
		for _, p := range prods {
			id, _ := p["identifier"].(string)
			priceID, _ := p["stripe_price_identifier"].(string)
			if id != "" && priceID != "" {
				renewalMap[id] = priceID
			}
		}
	}

	if len(renewalMap) > 0 {
		var items []*stripeAPI.SubscriptionItemsParams
		hasRenewal := false
		for _, item := range sub.Items.Data {
			prodID := item.Price.Product.ID
			if renewalPrice, ok := renewalMap[prodID]; ok && renewalPrice != "" {
				hasRenewal = true
				items = append(items, &stripeAPI.SubscriptionItemsParams{
					ID:       stripeAPI.String(item.ID),
					Price:    stripeAPI.String(renewalPrice),
					Quantity: stripeAPI.Int64(item.Quantity),
				})
			} else {
				items = append(items, &stripeAPI.SubscriptionItemsParams{
					ID:       stripeAPI.String(item.ID),
					Quantity: stripeAPI.Int64(item.Quantity),
				})
			}
		}

		if hasRenewal {
			_ = c.ModifySubscription(sub.ID, items)
			slog.Info("stripe: subscription updated on paid alert", "sub_id", sub.ID)
		}
	}

	return map[string]interface{}{"success": true}, nil
}

func getMockProducts(isRenew int) map[string]interface{} {
	return map[string]interface{}{
		"products": []map[string]interface{}{
			{
				"identifier":              "prod_mock_basic",
				"stripe_price_identifier": "price_mock_basic",
				"stripe_name":             "Básico",
				"price":                   99900,
			},
			{
				"identifier":              "prod_mock_pro",
				"stripe_price_identifier": "price_mock_pro",
				"stripe_name":             "Profesional",
				"price":                   199900,
			},
			{
				"identifier":              "prod_mock_enterprise",
				"stripe_price_identifier": "price_mock_enterprise",
				"stripe_name":             "Empresarial",
				"price":                   499900,
			},
		},
	}
}
