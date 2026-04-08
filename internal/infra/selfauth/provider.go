package selfauth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"golang.org/x/crypto/bcrypt"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// Provider implements port.IdentityProvider with bcrypt passwords and self-issued JWTs.
// No external IdP dependency — works on any cloud or local.
type Provider struct {
	db         *bun.DB
	signingKey []byte
	issuer     string
	audience   string
	tokenTTL   time.Duration
	refreshTTL time.Duration
}

type Config struct {
	DB         *bun.DB
	SigningKey string
	Issuer     string
	Audience   string
	TokenTTL   time.Duration
	RefreshTTL time.Duration
}

func normEmail(s string) string {
	return strings.TrimSpace(strings.ToLower(s))
}

func New(cfg Config) *Provider {
	if cfg.Issuer == "" {
		cfg.Issuer = "solucioncp-selfauth"
	}
	if cfg.Audience == "" {
		cfg.Audience = "solucioncp"
	}
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 1 * time.Hour
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	return &Provider{
		db:         cfg.DB,
		signingKey: []byte(cfg.SigningKey),
		issuer:     cfg.Issuer,
		audience:   cfg.Audience,
		tokenTTL:   cfg.TokenTTL,
		refreshTTL: cfg.RefreshTTL,
	}
}

type userRow struct {
	bun.BaseModel `bun:"table:user,alias:user"`
	ID            int64   `bun:"id,pk"`
	Email         string  `bun:"email"`
	Name          *string `bun:"name"`
	CognitoSub    *string `bun:"cognito_sub"`
	PasswordHash  *string `bun:"password_hash"`
}

func (p *Provider) InitiateAuth(ctx context.Context, flow string, params map[string]string) (*port.InitiateAuthResult, error) {
	switch flow {
	case "USER_PASSWORD_AUTH":
		return p.passwordAuth(ctx, params["USERNAME"], params["PASSWORD"])
	case "REFRESH_TOKEN_AUTH":
		return p.refreshAuth(ctx, params["REFRESH_TOKEN"])
	default:
		return nil, fmt.Errorf("unsupported auth flow: %s", flow)
	}
}

func (p *Provider) passwordAuth(ctx context.Context, email, password string) (*port.InitiateAuthResult, error) {
	email = normEmail(email)
	var u userRow
	err := p.db.NewSelect().Model(&u).Where("lower(trim(email)) = ?", email).Limit(1).Scan(ctx)
	if err != nil {
		// #region agent log
		debugLog("28c9f7", "selfauth/provider.go:passwordAuth", "user_not_found", map[string]any{"email": email, "err": err.Error()})
		// #endregion
		return nil, fmt.Errorf("invalid credentials")
	}
	if u.PasswordHash == nil || *u.PasswordHash == "" {
		// #region agent log
		debugLog("28c9f7", "selfauth/provider.go:passwordAuth", "password_hash_null", map[string]any{"email": email, "user_id": u.ID})
		// #endregion
		return nil, fmt.Errorf("password not set for this account")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(password)); err != nil {
		// #region agent log
		debugLog("28c9f7", "selfauth/provider.go:passwordAuth", "bcrypt_mismatch", map[string]any{"email": email, "user_id": u.ID})
		// #endregion
		return nil, fmt.Errorf("invalid credentials")
	}
	// #region agent log
	debugLog("28c9f7", "selfauth/provider.go:passwordAuth", "auth_success", map[string]any{"email": email, "user_id": u.ID})
	// #endregion
	tokens, err := p.issueTokens(u)
	if err != nil {
		return nil, err
	}
	return &port.InitiateAuthResult{Tokens: tokens}, nil
}

func (p *Provider) refreshAuth(ctx context.Context, refreshToken string) (*port.InitiateAuthResult, error) {
	claims, err := p.parseToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, fmt.Errorf("missing sub in refresh token")
	}
	var u userRow
	err = p.db.NewSelect().Model(&u).Where("cognito_sub = ?", sub).Limit(1).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}
	tokens, err := p.issueTokens(u)
	if err != nil {
		return nil, err
	}
	return &port.InitiateAuthResult{Tokens: tokens}, nil
}

func (p *Provider) RespondToAuthChallenge(_ context.Context, _, _, _, _ string) (*port.RespondChallengeResult, error) {
	return nil, fmt.Errorf("challenges not supported in self-managed auth")
}

func (p *Provider) SignUp(ctx context.Context, email, password string) (string, error) {
	email = normEmail(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	sub := uuid.New().String()
	hashStr := string(hash)
	_, err = p.db.NewUpdate().TableExpr(`"user"`).
		Set("password_hash = ?", hashStr).
		Set("cognito_sub = ?", sub).
		Where("lower(trim(email)) = ?", email).
		Exec(ctx)
	if err != nil {
		return "", fmt.Errorf("update user password: %w", err)
	}
	return sub, nil
}

func (p *Provider) ChangePassword(ctx context.Context, accessToken, currentPassword, newPassword string) error {
	claims, err := p.parseToken(accessToken)
	if err != nil {
		return fmt.Errorf("invalid access token: %w", err)
	}
	sub, _ := claims["sub"].(string)
	var u userRow
	if err := p.db.NewSelect().Model(&u).Where("cognito_sub = ?", sub).Limit(1).Scan(ctx); err != nil {
		return fmt.Errorf("user not found")
	}
	if u.PasswordHash == nil || *u.PasswordHash == "" {
		return fmt.Errorf("no password set")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*u.PasswordHash), []byte(currentPassword)); err != nil {
		return fmt.Errorf("current password is incorrect")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	hashStr := string(hash)
	_, err = p.db.NewUpdate().TableExpr(`"user"`).
		Set("password_hash = ?", hashStr).
		Where("cognito_sub = ?", sub).
		Exec(ctx)
	return err
}

func (p *Provider) ForgotPassword(_ context.Context, email string) (*port.CodeDeliveryDetails, error) {
	// In self-managed mode, password reset is simplified:
	// a real implementation would email a code. For now, return a stub.
	return &port.CodeDeliveryDetails{
		Destination:    maskEmail(email),
		DeliveryMedium: "EMAIL",
		AttributeName:  "email",
	}, nil
}

func (p *Provider) ConfirmForgotPassword(ctx context.Context, email, _ /*verificationCode*/, newPassword string) error {
	email = normEmail(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	hashStr := string(hash)
	res, err := p.db.NewUpdate().TableExpr(`"user"`).
		Set("password_hash = ?", hashStr).
		Where("lower(trim(email)) = ?", email).
		Exec(ctx)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (p *Provider) AdminCreateUser(ctx context.Context, email, tempPassword string) (string, error) {
	email = normEmail(email)
	hash, err := bcrypt.GenerateFromPassword([]byte(tempPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	sub := uuid.New().String()
	hashStr := string(hash)
	_, err = p.db.NewUpdate().TableExpr(`"user"`).
		Set("password_hash = ?", hashStr).
		Set("cognito_sub = ?", sub).
		Where("lower(trim(email)) = ?", email).
		Exec(ctx)
	if err != nil {
		return "", err
	}
	return sub, nil
}

func (p *Provider) issueTokens(u userRow) (*port.AuthTokens, error) {
	now := time.Now()
	sub := ""
	if u.CognitoSub != nil {
		sub = *u.CognitoSub
	}
	name := u.Email
	if u.Name != nil {
		name = *u.Name
	}

	accessClaims := jwt.MapClaims{
		"sub":       sub,
		"email":     u.Email,
		"name":      name,
		"iss":       p.issuer,
		"aud":       p.audience,
		"iat":       now.Unix(),
		"exp":       now.Add(p.tokenTTL).Unix(),
		"token_use": "access",
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(p.signingKey)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	idClaims := jwt.MapClaims{
		"sub":       sub,
		"email":     u.Email,
		"name":      name,
		"iss":       p.issuer,
		"aud":       p.audience,
		"iat":       now.Unix(),
		"exp":       now.Add(p.tokenTTL).Unix(),
		"token_use": "id",
	}
	idToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, idClaims).SignedString(p.signingKey)
	if err != nil {
		return nil, fmt.Errorf("sign id token: %w", err)
	}

	refreshClaims := jwt.MapClaims{
		"sub":       sub,
		"iss":       p.issuer,
		"iat":       now.Unix(),
		"exp":       now.Add(p.refreshTTL).Unix(),
		"token_use": "refresh",
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(p.signingKey)
	if err != nil {
		return nil, fmt.Errorf("sign refresh token: %w", err)
	}

	return &port.AuthTokens{
		AccessToken:  accessToken,
		IDToken:      idToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int32(p.tokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (p *Provider) parseToken(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(_ *jwt.Token) (interface{}, error) {
		return p.signingKey, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

func (p *Provider) SigningKey() []byte { return p.signingKey }
func (p *Provider) Issuer() string     { return p.issuer }
func (p *Provider) Audience() string   { return p.audience }

func maskEmail(email string) string {
	for i, c := range email {
		if c == '@' {
			if i <= 2 {
				return "***" + email[i:]
			}
			return email[:2] + "***" + email[i:]
		}
	}
	return "***"
}

func generateSecureKey() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// GenerateSigningKey returns a 64-char hex key for use as SELFAUTH_SIGNING_KEY.
func GenerateSigningKey() string { return generateSecureKey() }

// #region agent log
func debugLog(session, location, message string, data map[string]any) {
	slog.Warn("[debug-"+session+"] "+message, "location", location, "data", data)
}

// #endregion
