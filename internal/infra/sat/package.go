package sat

import (
	"encoding/base64"
	"fmt"
	"log/slog"
)

// Package represents a downloadable ZIP package from the SAT descarga masiva service.
// Matches Python Package class.
type Package struct {
	Identifier  string
	RequestType RequestType

	// Binary is the decoded ZIP content after download.
	Binary []byte

	// Raw is the full HTTP response body.
	Raw []byte
}

// NewPackage creates a Package with the given ID and request type.
func NewPackage(id string, requestType RequestType) *Package {
	return &Package{
		Identifier:  id,
		RequestType: requestType,
	}
}

// PackagesFromIDs creates a slice of packages from a list of IDs.
// Matches Python Package.from_ids.
func PackagesFromIDs(ids []string, requestType RequestType) []*Package {
	pkgs := make([]*Package, len(ids))
	for i, id := range ids {
		pkgs[i] = NewPackage(id, requestType)
	}
	return pkgs
}

// Download fetches the package content from SAT. If process is true, the Binary field
// will be populated with the decoded ZIP content.
// Matches Python Package.download.
func (p *Package) Download(c *Connector, process bool) error {
	data := map[string]string{
		"package_id": p.Identifier,
		"signature":  "{signature}",
	}

	slog.Debug("sat: downloading package", "id", p.Identifier)

	resp, err := c.downloadPackage(data)
	if err != nil {
		return fmt.Errorf("download package %s: %w", p.Identifier, err)
	}

	p.Raw = resp.Body

	if err := p.computeBinary(); err != nil {
		return err
	}

	slog.Debug("sat: package downloaded",
		"id", p.Identifier,
		"size_bytes", len(p.Binary),
	)

	return nil
}

// computeBinary decodes the base64 ZIP content from the SOAP response.
// Matches Python Package._compute_binary.
func (p *Package) computeBinary() error {
	parsed, err := parseDownloadResponse(p.Raw)
	if err != nil {
		return fmt.Errorf("parse download response for %s: %w", p.Identifier, err)
	}

	if parsed.CodEstatus == 5008 {
		return &TooManyDownloadsError{}
	}

	if parsed.Content == "" {
		return fmt.Errorf("empty content in download response for package %s", p.Identifier)
	}

	decoded, err := base64.StdEncoding.DecodeString(parsed.Content)
	if err != nil {
		return fmt.Errorf("decode base64 content for %s: %w", p.Identifier, err)
	}

	p.Binary = decoded
	return nil
}
