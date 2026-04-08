package sat

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// LoginHandler manages SAT authentication tokens with automatic refresh.
// Matches Python SATLoginHandler.
type LoginHandler struct {
	ch *CertificateHandler

	mu           sync.Mutex
	token        string
	tokenExpires time.Time
	timeout      time.Duration
}

// NewLoginHandler creates a LoginHandler for the given certificate.
func NewLoginHandler(ch *CertificateHandler, timeout time.Duration) *LoginHandler {
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &LoginHandler{
		ch:      ch,
		timeout: timeout,
	}
}

// Token returns a valid SAT authentication token, requesting a new one if expired.
// Matches Python SATLoginHandler.token property.
func (lh *LoginHandler) Token() (string, error) {
	lh.mu.Lock()
	defer lh.mu.Unlock()

	if !lh.tokenExpired() {
		return lh.token, nil
	}

	if err := lh.login(time.Time{}, time.Time{}, ""); err != nil {
		return "", err
	}
	return lh.token, nil
}

// ForceLogin forces a token refresh regardless of expiration.
func (lh *LoginHandler) ForceLogin() error {
	lh.mu.Lock()
	defer lh.mu.Unlock()
	return lh.login(time.Time{}, time.Time{}, "")
}

func (lh *LoginHandler) tokenExpired() bool {
	return lh.token == "" || time.Now().UTC().After(lh.tokenExpires)
}

// login sends a login request to the SAT authentication endpoint.
// Matches Python SATLoginHandler._login.
func (lh *LoginHandler) login(created, expires time.Time, uuidStr string) error {
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if expires.IsZero() {
		expires = created.Add(5 * time.Minute)
	}
	if uuidStr == "" {
		uuidStr = fmt.Sprintf("uuid-%s-1", uuid.New().String())
	}

	slog.Debug("sat: requesting new auth token", "created", created, "expires", expires)

	body, err := lh.buildLoginSOAPBody(created, expires, uuidStr)
	if err != nil {
		return fmt.Errorf("build login body: %w", err)
	}

	resp, err := soapConsume(soapActionAutentica, urlAutenticacion, body, "", lh.timeout)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	if err := checkResponse(resp); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	parsed, err := parseLoginResponse(resp.Body)
	if err != nil {
		return fmt.Errorf("login parse: %w", err)
	}

	lh.token = parsed.Token
	lh.tokenExpires = expires

	slog.Debug("sat: auth token obtained", "expires", expires)
	return nil
}

// buildLoginSOAPBody creates the SOAP request body for authentication.
// Matches Python SATLoginHandler._get_login_soap_body.
func (lh *LoginHandler) buildLoginSOAPBody(created, expires time.Time, uuidStr string) (string, error) {
	createdStr := created.Format("2006-01-02T15:04:05.000Z")
	expiresStr := expires.Format("2006-01-02T15:04:05.000Z")

	// Build timestamp.
	timestamp := templateReplace(tplTimestamp, map[string]string{
		"created": createdStr,
		"expires": expiresStr,
	})

	// Digest the timestamp.
	digestValue := digest(timestamp)

	// Build SignedInfo (with URI #_0 for the timestamp reference).
	signedInfo := templateReplace(tplSignedInfo, map[string]string{
		"uri":          "#_0",
		"digest_value": digestValue,
	})

	// Sign the SignedInfo.
	signatureValue, err := lh.ch.Sign(signedInfo)
	if err != nil {
		return "", err
	}

	// Assemble the login envelope.
	envelope := templateReplace(tplLoginEnvelope, map[string]string{
		"binary_security_token": lh.ch.certBase64,
		"digest_value":          digestValue,
		"signature_value":       signatureValue,
		"uuid":                  uuidStr,
		"timestamp_node":        timestamp,
		"signed_info_node":      signedInfo,
	})

	return envelope, nil
}
