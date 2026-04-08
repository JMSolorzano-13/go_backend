package sat

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/siigofiscal/go_backend/internal/domain/company"
)

// SAT X.500 OID for x500UniqueIdentifier = 2.5.4.45
var oidX500UniqueID = asn1.ObjectIdentifier{2, 5, 4, 45}

// CertificateHandler holds a parsed FIEL certificate and its decrypted private key,
// providing methods needed by the SAT SOAP signing process.
type CertificateHandler struct {
	// certBase64 is the base64-encoded DER certificate (no PEM header/footer),
	// used directly in SOAP envelopes.
	certBase64 string

	// cert is the parsed X.509 certificate.
	cert *x509.Certificate

	// privateKey is the decrypted RSA private key.
	privateKey *rsa.PrivateKey

	// RFC extracted from the certificate's x500UniqueIdentifier.
	RFC string

	// Issuer is the formatted issuer string for XML signatures (C=MX,O=...).
	Issuer string

	// SerialNumber is the certificate serial as a decimal string.
	SerialNumber string
}

// NewCertificateHandler creates a CertificateHandler from raw DER certificate,
// DER encrypted private key, and the key passphrase.
// This is the Go equivalent of Python's CertificateHandler.__init__.
func NewCertificateHandler(certDER, keyDER, password []byte) (*CertificateHandler, error) {
	// Parse the X.509 certificate from DER.
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse certificate: %w", err)
	}

	// Decrypt the PKCS#8 private key.
	privKeyRaw, err := company.ParseEncryptedPKCS8(keyDER, password)
	if err != nil {
		return nil, fmt.Errorf("decrypt private key: %w", err)
	}

	rsaKey, ok := privKeyRaw.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("expected RSA private key, got %T", privKeyRaw)
	}

	// Extract RFC from certificate subject.
	rfc := extractRFCFromCert(cert)
	if rfc == "" {
		return nil, fmt.Errorf("RFC not found in certificate subject")
	}

	// Format issuer string matching Python: "C=MX,O=SAT,..."
	issuer := formatIssuer(cert)
	if issuer == "" {
		return nil, fmt.Errorf("issuer not found in certificate")
	}

	// Base64-encode the DER certificate for use in XML.
	certB64 := base64.StdEncoding.EncodeToString(certDER)

	return &CertificateHandler{
		certBase64:   certB64,
		cert:         cert,
		privateKey:   rsaKey,
		RFC:          rfc,
		Issuer:       issuer,
		SerialNumber: cert.SerialNumber.Text(10),
	}, nil
}

// Sign signs data using SHA1 with PKCS1v15 padding and returns the base64-encoded signature.
// Matches Python CertificateHandler.sign.
func (ch *CertificateHandler) Sign(data string) (string, error) {
	hash := sha1.Sum([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ch.privateKey, crypto.SHA1, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

// CertBase64 returns the certificate as a single-line base64 string (no newlines).
func (ch *CertificateHandler) CertBase64() string {
	return ch.certBase64
}

// extractRFCFromCert gets the RFC from the certificate's x500UniqueIdentifier field.
// Matches Python _get_rfc_from_cert.
func extractRFCFromCert(cert *x509.Certificate) string {
	for _, name := range cert.Subject.Names {
		if name.Type.Equal(oidX500UniqueID) {
			if s, ok := name.Value.(string); ok {
				// Split on "/" and take the first part, trimming spaces.
				parts := strings.SplitN(s, "/", 2)
				return strings.TrimSpace(parts[0])
			}
		}
	}
	// Fallback to SerialNumber.
	if cert.Subject.SerialNumber != "" {
		return cert.Subject.SerialNumber
	}
	return ""
}

// formatIssuer builds the issuer string matching Python's _get_issuer_from_cert:
// "C=MX,O=SAT,OU=...,CN=..." with URL-encoded values.
func formatIssuer(cert *x509.Certificate) string {
	components := cert.Issuer.Names
	parts := make([]string, 0, len(components))
	for _, attr := range components {
		key := oidShortName(attr.Type)
		val := fmt.Sprintf("%v", attr.Value)
		parts = append(parts, key+"="+url.QueryEscape(val))
	}
	return strings.Join(parts, ",")
}

// oidShortName returns the common short name for a well-known X.500 OID.
func oidShortName(oid asn1.ObjectIdentifier) string {
	switch {
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 3}):
		return "CN"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 6}):
		return "C"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 7}):
		return "L"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 8}):
		return "ST"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 10}):
		return "O"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 11}):
		return "OU"
	case oid.Equal(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 1}):
		return "emailAddress"
	case oid.Equal(asn1.ObjectIdentifier{2, 5, 4, 45}):
		return "x500UniqueIdentifier"
	default:
		return oid.String()
	}
}

// HandleSpecialCharactersInRFC replaces Ñ with its XML entity &#209;.
// Matches Python handle_special_characters_in_rfc.
func HandleSpecialCharactersInRFC(rfc string) string {
	return strings.ReplaceAll(rfc, "Ñ", "&#209;")
}

// EscapeXML escapes characters that are not safe in XML attribute values.
func EscapeXML(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&apos;",
	)
	return r.Replace(s)
}
