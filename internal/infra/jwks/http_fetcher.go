package jwks

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// HTTPFetcher implements domain auth JWKS loading via HTTP GET.
type HTTPFetcher struct {
	Client *http.Client
}

// FetchJWKS downloads JWKS JSON from url.
func (f *HTTPFetcher) FetchJWKS(ctx context.Context, url string) ([]byte, error) {
	c := f.Client
	if c == nil {
		c = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch JWKS: status %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
