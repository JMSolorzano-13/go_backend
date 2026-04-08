package sat

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout is the HTTP request timeout for SAT web service calls (seconds).
const DefaultTimeout = 15 * time.Second

// SOAPResponse holds the raw HTTP response data from a SAT SOAP call.
type SOAPResponse struct {
	StatusCode int
	Body       []byte
}

// soapConsume sends a SOAP request to the SAT web service.
// Matches Python utils.consume.
func soapConsume(soapAction, uri, body string, token string, timeout time.Duration) (*SOAPResponse, error) {
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	client := &http.Client{Timeout: timeout}

	req, err := http.NewRequest(http.MethodPost, uri, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("Accept", "text/xml")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("SOAPAction", soapAction)

	if token != "" {
		req.Header.Set("Authorization", fmt.Sprintf(`WRAP access_token="%s"`, token))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("soap request to %s: %w", uri, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response from %s: %w", uri, err)
	}

	return &SOAPResponse{
		StatusCode: resp.StatusCode,
		Body:       data,
	}, nil
}

// checkResponse validates the SOAP response status code.
// Matches Python utils.check_response.
func checkResponse(resp *SOAPResponse) error {
	if resp.StatusCode != http.StatusOK {
		return &RequestError{
			StatusCode: resp.StatusCode,
			Reason:     http.StatusText(resp.StatusCode),
		}
	}
	return nil
}
