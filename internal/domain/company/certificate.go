package company

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"fmt"
	"time"
)

// CertInfo holds the parsed fields returned by GET /api/Company/get_cer.
type CertInfo struct {
	RFC          string `json:"rfc"`
	Name         string `json:"name"`
	NotBefore    string `json:"not_before"`
	NotAfter     string `json:"not_after"`
	SerialNumber string `json:"serial_number"`
}

// ParsedCertificate wraps an x509.Certificate with SAT-specific accessors.
type ParsedCertificate struct {
	Cert *x509.Certificate
	RFC  string
	Name string
}

// SAT X.500 OID for x500UniqueIdentifier = 2.5.4.45
var oidX500UniqueIdentifier = asn1.ObjectIdentifier{2, 5, 4, 45}

// ParseCertificateDER parses a DER-encoded X.509 certificate and extracts SAT fields.
func ParseCertificateDER(der []byte) (*ParsedCertificate, error) {
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("not a proper certificate: %w", err)
	}

	rfc := extractRFC(cert)
	name := cert.Subject.CommonName

	return &ParsedCertificate{Cert: cert, RFC: rfc, Name: name}, nil
}

// Info returns the CertInfo for JSON serialization.
func (p *ParsedCertificate) Info() CertInfo {
	return CertInfo{
		RFC:          p.RFC,
		Name:         p.Name,
		NotBefore:    p.Cert.NotBefore.Format("2006-01-02T15:04:05"),
		NotAfter:     p.Cert.NotAfter.Format("2006-01-02T15:04:05"),
		SerialNumber: p.Cert.SerialNumber.Text(10),
	}
}

// IsFIEL returns true if the certificate is a FIEL (not a CSD).
// FIEL certs have KeyUsage with both DigitalSignature and ContentCommitment (NonRepudiation).
// CSD certs have DigitalSignature only.
func (p *ParsedCertificate) IsFIEL() bool {
	ku := p.Cert.KeyUsage
	return ku&x509.KeyUsageDigitalSignature != 0 && ku&x509.KeyUsageContentCommitment != 0
}

// CheckValidity returns an error if the certificate is expired or not yet valid.
func (p *ParsedCertificate) CheckValidity() error {
	now := time.Now()
	if now.After(p.Cert.NotAfter) {
		return fmt.Errorf("Certificate is expired")
	}
	if now.Before(p.Cert.NotBefore) {
		return fmt.Errorf("Certificate is not yet valid")
	}
	return nil
}

// PublicKeyBytes returns the DER-encoded PKIX public key for comparison.
func (p *ParsedCertificate) PublicKeyBytes() ([]byte, error) {
	return x509.MarshalPKIXPublicKey(p.Cert.PublicKey)
}

// extractRFC gets the RFC from the certificate's subject.
// SAT puts the RFC in the x500UniqueIdentifier (OID 2.5.4.45) field,
// or in SerialNumber as fallback.
func extractRFC(cert *x509.Certificate) string {
	for _, name := range cert.Subject.Names {
		if name.Type.Equal(oidX500UniqueIdentifier) {
			if s, ok := name.Value.(string); ok {
				return extractRFCFromUniqueID(s)
			}
		}
	}
	if cert.Subject.SerialNumber != "" {
		return cert.Subject.SerialNumber
	}
	return ""
}

// extractRFCFromUniqueID extracts the RFC portion from SAT's x500UniqueIdentifier.
// The field often contains " / CURP..." after the RFC; we take just the first segment.
func extractRFCFromUniqueID(uid string) string {
	for i, c := range uid {
		if c == '/' || c == ' ' {
			trimmed := uid[:i]
			if len(trimmed) >= 12 {
				return trimmed
			}
		}
	}
	return uid
}

// ValidatePrivateKey decrypts a PKCS#8 encrypted DER private key with the given password
// and verifies it matches the certificate's public key.
func ValidatePrivateKey(keyDER []byte, password string, cert *ParsedCertificate) error {
	// Parse the encrypted PKCS#8 private key.
	privKey, err := ParseEncryptedPKCS8(keyDER, []byte(password))
	if err != nil {
		return fmt.Errorf("Invalid private key, maybe the passphrase is wrong: %w", err)
	}

	// Extract public key from the private key and compare with cert's public key.
	certPubBytes, err := cert.PublicKeyBytes()
	if err != nil {
		return fmt.Errorf("extract cert public key: %w", err)
	}

	privPub, err := publicKeyFromPrivate(privKey)
	if err != nil {
		return err
	}

	privPubBytes, err := x509.MarshalPKIXPublicKey(privPub)
	if err != nil {
		return fmt.Errorf("marshal private key's public key: %w", err)
	}

	if !bytesEqual(certPubBytes, privPubBytes) {
		return fmt.Errorf("The private key does not match the certificate")
	}
	return nil
}

// ValidateFIELCertificate runs the full validation chain matching Python's
// get_certificate_and_validate_private_key.
func ValidateFIELCertificate(cerDER, keyDER []byte, password string) (*ParsedCertificate, error) {
	cert, err := ParseCertificateDER(cerDER)
	if err != nil {
		return nil, err
	}
	if !cert.IsFIEL() {
		return nil, fmt.Errorf("The certificate is not a FIEL certificate")
	}
	if err := cert.CheckValidity(); err != nil {
		return nil, err
	}
	if err := ValidatePrivateKey(keyDER, password, cert); err != nil {
		return nil, err
	}
	return cert, nil
}

func publicKeyFromPrivate(priv interface{}) (interface{}, error) {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey, nil
	case *ecdsa.PrivateKey:
		return &k.PublicKey, nil
	case ed25519.PrivateKey:
		return k.Public(), nil
	default:
		return nil, fmt.Errorf("unsupported private key type: %T", priv)
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
