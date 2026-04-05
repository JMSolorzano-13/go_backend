package port

import "context"

// AuthTokens is a successful OIDC/password auth result (Cognito AuthenticationResult shape).
//
// JSON for /api/User/auth and /api/User/auth_challenge must stay Chalice-compatible:
// AccessToken, IdToken, RefreshToken, ExpiresIn, TokenType (PascalCase).
type AuthTokens struct {
	AccessToken  string `json:"AccessToken"`
	IDToken      string `json:"IdToken"`
	RefreshToken string `json:"RefreshToken"`
	ExpiresIn    int32  `json:"ExpiresIn"`
	TokenType    string `json:"TokenType"`
}

// CodeDeliveryDetails describes where a verification code was sent (forgot password).
//
// Call-site map: user.Forgot — previously Cognito CodeDeliveryDetails.
type CodeDeliveryDetails struct {
	Destination    string
	DeliveryMedium string
	AttributeName  string
}

// InitiateAuthResult is the outcome of starting a user auth flow.
//
// Call-site map: user.Auth — if ChallengeName non-empty, return 428 with session;
// else require Tokens non-nil for 200 JSON.
type InitiateAuthResult struct {
	ChallengeName    string
	ChallengeSession string
	Tokens           *AuthTokens
}

// RespondChallengeResult completes a challenge such as NEW_PASSWORD_REQUIRED.
//
// Call-site map: user.AuthChallenge.
type RespondChallengeResult struct {
	Tokens *AuthTokens
}

// IdentityProvider abstracts a user pool / IdP (Cognito, Azure AD B2C).
//
// Call-site map (internal/handler/user.go):
//
//	InitiateAuth, RespondToAuthChallenge, SignUp, ChangePassword,
//	ForgotPassword, ConfirmForgotPassword, AdminCreateUser.
//
// OAuth2 code exchange stays HTTP in the handler (not this interface).
//
// Infra mapping: internal/infra/cognito.Client methods of the same names,
// with AWS SDK outputs mapped into the structs above in Phase 1E.
type IdentityProvider interface {
	InitiateAuth(ctx context.Context, flow string, params map[string]string) (*InitiateAuthResult, error)
	RespondToAuthChallenge(ctx context.Context, challengeName, session, email, newPassword string) (*RespondChallengeResult, error)
	SignUp(ctx context.Context, email, password string) (userSub string, err error)
	// ChangePassword and ConfirmForgotPassword: on success the HTTP API returns {} (empty object),
	// not provider-specific metadata. Adapters return nil error only; handlers own the JSON body.
	ChangePassword(ctx context.Context, accessToken, currentPassword, newPassword string) error
	ForgotPassword(ctx context.Context, email string) (*CodeDeliveryDetails, error)
	ConfirmForgotPassword(ctx context.Context, email, verificationCode, newPassword string) error
	AdminCreateUser(ctx context.Context, email, tempPassword string) (userSub string, err error)
}
