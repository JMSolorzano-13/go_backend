package pasto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client wraps all HTTP calls to the PastoCorp/ADD API.
// Matches the Python PastoRequest / PastoRequestAuth / Dashboard / WorkerCreator /
// WorkerConfigurator / CompanyRequester / ResetLicense hierarchy.
type Client struct {
	url        string
	ocpKey     string
	httpClient *http.Client
}

func NewClient(baseURL, ocpKey string, timeoutSecs int) *Client {
	return &Client{
		url:    baseURL,
		ocpKey: ocpKey,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSecs) * time.Second,
		},
	}
}

// baseHeaders returns the common Pasto API headers.
func (c *Client) baseHeaders() map[string]string {
	return map[string]string{
		"Content-Type":               "application/json",
		"Ocp-Apim-Subscription-Key": c.ocpKey,
	}
}

// authedHeaders returns headers with Authorization token.
func (c *Client) authedHeaders(token string) map[string]string {
	h := c.baseHeaders()
	h["Authorization"] = token
	return h
}

func (c *Client) doRequest(method, url string, headers map[string]string, body []byte) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("pasto: build request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("pasto: http: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return respBody, resp.StatusCode, nil
}

// Login authenticates with the Pasto dashboard and returns a bearer token.
// Matches Dashboard._get_token(email, password).
func (c *Client) Login(email, password string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"email": email, "password": password})
	url := fmt.Sprintf("%s/usuario/login", c.url)
	body, status, err := c.doRequest("POST", url, c.baseHeaders(), payload)
	if err != nil {
		return "", err
	}
	if status != 200 {
		slog.Warn("pasto: login failed", "status", status)
		return "", fmt.Errorf("pasto: login status %d", status)
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("pasto: login decode: %w", err)
	}
	return resp.Data, nil
}

// CreateWorker creates a new ADD worker for a workspace.
// Matches WorkerCreator._create_in_pasto.
type WorkerResult struct {
	PastoID      string
	SerialNumber string
	Token        string
}

func (c *Client) CreateWorker(token, subscriptionID, dashboardID, workspaceIdentifier string) (*WorkerResult, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":                      workspaceIdentifier,
		"description":               workspaceIdentifier,
		"status":                    1,
		"purchasedsubcription_id":   subscriptionID,
		"dashboard_id":              dashboardID,
	})
	url := fmt.Sprintf("%s/worker", c.url)
	body, status, err := c.doRequest("POST", url, c.authedHeaders(token), payload)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("pasto: create_worker status %d: %s", status, string(body))
	}
	var resp struct {
		Data struct {
			ID           string `json:"_id"`
			SerialNumber string `json:"serial_number"`
			APIKeys      struct {
				Production struct {
					WorkerToken string `json:"worker_token"`
				} `json:"production"`
			} `json:"api_keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("pasto: create_worker decode: %w", err)
	}
	return &WorkerResult{
		PastoID:      resp.Data.ID,
		SerialNumber: resp.Data.SerialNumber,
		Token:        resp.Data.APIKeys.Production.WorkerToken,
	}, nil
}

// SetWorkerCredentials configures a worker's DB connector.
// Matches WorkerConfigurator.set_credentials.
func (c *Client) SetWorkerCredentials(token, workerID, workspaceIdentifier, server, username, password string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"_id": workerID,
		"connectors": []map[string]interface{}{
			{
				"connector_alias": "sql-001",
				"identifier":      workspaceIdentifier,
				"entries": []map[string]string{
					{"key": "username", "value": username},
					{"key": "password", "value": password},
					{"key": "server", "value": server},
				},
			},
		},
	})
	url := fmt.Sprintf("%s/worker/connector", c.url)
	_, status, err := c.doRequest("PUT", url, c.authedHeaders(token), payload)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("pasto: set_credentials status %d", status)
	}
	return nil
}

// RequestCompanies triggers the ADD worker to push company list to our webhook.
// Matches CompanyRequester.request_companies.
func (c *Client) RequestCompanies(workerToken, workspaceIdentifier, workerID, selfEndpoint, apiRoute string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"action_code": "contpaqi-add-get-companies-sql",
		"parameters": map[string]interface{}{
			"dbconfiguration": map[string]string{"databasename": "DB_Directory"},
			"Receiver": map[string]interface{}{
				"Endpoint": selfEndpoint,
				"ApiRoute": apiRoute,
				"Method":   1,
				"Headers": []map[string]string{
					{"Key": "workspace_identifier", "Value": workspaceIdentifier},
					{"Key": "worker_id", "Value": workerID},
				},
			},
		},
		"connector_ids": []string{workspaceIdentifier},
	})
	url := fmt.Sprintf("%s/syncWorkerActions/add", c.url)
	_, status, err := c.doRequest("POST", url, c.authedHeaders(workerToken), payload)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("pasto: request_companies status %d", status)
	}
	return nil
}

// ResetLicense calls the ADD reset license endpoint.
// Matches ResetLicense._reset_license.
func (c *Client) ResetLicense(token, licenseKey string) (int, error) {
	url := fmt.Sprintf("%s/%s/reset", c.url, licenseKey)
	headers := c.authedHeaders(token)
	headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	_, status, err := c.doRequest("POST", url, headers, []byte(licenseKey))
	if err != nil {
		return 0, err
	}
	return status, nil
}
