package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/siigofiscal/go_backend/internal/config"
)

// JWKSFetcher loads raw JWKS JSON (e.g. Cognito). Implemented outside domain.
type JWKSFetcher interface {
	FetchJWKS(ctx context.Context, url string) ([]byte, error)
}

const LocalSecretKey = "local-dev-secret-key-do-not-use-in-production-this-is-only-for-testing"

type Claims struct {
	Sub   string
	Email string
	Name  string
}

type JWTDecoder struct {
	cfg       *config.Config
	jwksURL   string
	fetch     JWKSFetcher
	keyCache  map[string]*rsa.PublicKey
	mu        sync.RWMutex
	lastFetch time.Time
	cacheTTL  time.Duration

	// Self-managed auth (HS256) — used when CloudProvider=azure.
	selfAuthKey    []byte
	selfAuthIssuer string
	selfAuthAud    string
}

// NewJWTDecoder builds a JWT decoder for Cognito JWKS (RS256).
func NewJWTDecoder(cfg *config.Config, fetch JWKSFetcher) *JWTDecoder {
	var jwksURL string
	if !cfg.LocalInfra && cfg.CognitoUserPoolID != "" {
		jwksURL = fmt.Sprintf(
			"https://cognito-idp.%s.amazonaws.com/%s/.well-known/jwks.json",
			cfg.RegionName, cfg.CognitoUserPoolID,
		)
	}
	return &JWTDecoder{
		cfg:      cfg,
		jwksURL:  jwksURL,
		fetch:    fetch,
		keyCache: make(map[string]*rsa.PublicKey),
		cacheTTL: 1 * time.Hour,
	}
}

// NewJWTDecoderSelfAuth builds a JWT decoder for self-managed HS256 tokens.
func NewJWTDecoderSelfAuth(cfg *config.Config, signingKey []byte, issuer, audience string) *JWTDecoder {
	return &JWTDecoder{
		cfg:            cfg,
		keyCache:       make(map[string]*rsa.PublicKey),
		cacheTTL:       1 * time.Hour,
		selfAuthKey:    signingKey,
		selfAuthIssuer: issuer,
		selfAuthAud:    audience,
	}
}

func (d *JWTDecoder) Decode(tokenString string) (*Claims, error) {
	if d.cfg.LocalInfra {
		return d.decodeLocal(tokenString)
	}
	if len(d.selfAuthKey) > 0 {
		return d.decodeSelfAuth(tokenString)
	}
	return d.decodeProduction(tokenString)
}

func (d *JWTDecoder) decodeSelfAuth(tokenString string) (*Claims, error) {
	token, err := jwt.Parse(tokenString, func(_ *jwt.Token) (interface{}, error) {
		return d.selfAuthKey, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(d.selfAuthIssuer),
		jwt.WithAudience(d.selfAuthAud),
	)
	if err != nil {
		return nil, fmt.Errorf("selfauth token validation failed: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	return extractClaims(claims), nil
}

func (d *JWTDecoder) decodeLocal(tokenString string) (*Claims, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("invalid token format: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	return extractClaims(claims), nil
}

func (d *JWTDecoder) decodeProduction(tokenString string) (*Claims, error) {
	token, err := jwt.Parse(tokenString, d.keyFunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithAudience(d.cfg.CognitoClientID),
		jwt.WithIssuer(fmt.Sprintf(
			"https://cognito-idp.%s.amazonaws.com/%s",
			d.cfg.RegionName, d.cfg.CognitoUserPoolID,
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	return extractClaims(claims), nil
}

func extractClaims(claims jwt.MapClaims) *Claims {
	c := &Claims{}
	if v, ok := claims["sub"].(string); ok {
		c.Sub = v
	}
	if v, ok := claims["email"].(string); ok {
		c.Email = v
	}
	if v, ok := claims["name"].(string); ok {
		c.Name = v
	}
	return c
}

func (d *JWTDecoder) keyFunc(token *jwt.Token) (interface{}, error) {
	kid, ok := token.Header["kid"].(string)
	if !ok {
		return nil, fmt.Errorf("missing kid in token header")
	}
	return d.getKey(kid)
}

func (d *JWTDecoder) getKey(kid string) (*rsa.PublicKey, error) {
	d.mu.RLock()
	if key, ok := d.keyCache[kid]; ok && time.Since(d.lastFetch) < d.cacheTTL {
		d.mu.RUnlock()
		return key, nil
	}
	d.mu.RUnlock()
	return d.fetchAndCacheKey(kid)
}

func (d *JWTDecoder) fetchAndCacheKey(kid string) (*rsa.PublicKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if key, ok := d.keyCache[kid]; ok && time.Since(d.lastFetch) < d.cacheTTL {
		return key, nil
	}

	if d.fetch == nil {
		return nil, fmt.Errorf("JWKS fetcher not configured")
	}
	raw, err := d.fetch.FetchJWKS(context.Background(), d.jwksURL)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}

	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &jwks); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}

	d.keyCache = make(map[string]*rsa.PublicKey)
	d.lastFetch = time.Now()

	for _, k := range jwks.Keys {
		if k.Kty != "RSA" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		d.keyCache[k.Kid] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
	}

	key, ok := d.keyCache[kid]
	if !ok {
		return nil, fmt.Errorf("key with kid %q not found in JWKS", kid)
	}
	return key, nil
}

// CreateMockToken generates a local development HS256 JWT matching Python's create_mock_user_tokens.
func CreateMockToken(email, cognitoSub string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       cognitoSub,
		"name":      email,
		"email":     email,
		"aud":       "local_mock_client",
		"iss":       "http://localhost:4566",
		"exp":       now.Add(24 * time.Hour).Unix(),
		"iat":       now.Unix(),
		"token_use": "id",
		"auth_time": now.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(LocalSecretKey))
}
