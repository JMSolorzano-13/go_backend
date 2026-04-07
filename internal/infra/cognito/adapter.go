package cognito

import (
	"context"

	"github.com/siigofiscal/go_backend/internal/domain/port"
)

// Adapter wraps the AWS Cognito Client to satisfy port.IdentityProvider.
type Adapter struct{ c *Client }

func NewAdapter(c *Client) *Adapter { return &Adapter{c: c} }

func (a *Adapter) InitiateAuth(ctx context.Context, flow string, params map[string]string) (*port.InitiateAuthResult, error) {
	out, err := a.c.InitiateAuth(ctx, flow, params)
	if err != nil {
		return nil, err
	}
	res := &port.InitiateAuthResult{
		ChallengeName:    string(out.ChallengeName),
		ChallengeSession: derefStr(out.Session),
	}
	if out.AuthenticationResult != nil {
		res.Tokens = &port.AuthTokens{
			AccessToken:  derefStr(out.AuthenticationResult.AccessToken),
			IDToken:      derefStr(out.AuthenticationResult.IdToken),
			RefreshToken: derefStr(out.AuthenticationResult.RefreshToken),
			ExpiresIn:    out.AuthenticationResult.ExpiresIn,
			TokenType:    derefStr(out.AuthenticationResult.TokenType),
		}
	}
	return res, nil
}

func (a *Adapter) RespondToAuthChallenge(ctx context.Context, challengeName, session, email, newPassword string) (*port.RespondChallengeResult, error) {
	out, err := a.c.RespondToAuthChallenge(ctx, challengeName, session, email, newPassword)
	if err != nil {
		return nil, err
	}
	res := &port.RespondChallengeResult{}
	if out.AuthenticationResult != nil {
		res.Tokens = &port.AuthTokens{
			AccessToken:  derefStr(out.AuthenticationResult.AccessToken),
			IDToken:      derefStr(out.AuthenticationResult.IdToken),
			RefreshToken: derefStr(out.AuthenticationResult.RefreshToken),
			ExpiresIn:    out.AuthenticationResult.ExpiresIn,
			TokenType:    derefStr(out.AuthenticationResult.TokenType),
		}
	}
	return res, nil
}

func (a *Adapter) SignUp(ctx context.Context, email, password string) (string, error) {
	return a.c.SignUp(ctx, email, password)
}

func (a *Adapter) ChangePassword(ctx context.Context, accessToken, currentPassword, newPassword string) error {
	_, err := a.c.ChangePassword(ctx, accessToken, currentPassword, newPassword)
	return err
}

func (a *Adapter) ForgotPassword(ctx context.Context, email string) (*port.CodeDeliveryDetails, error) {
	out, err := a.c.ForgotPassword(ctx, email)
	if err != nil {
		return nil, err
	}
	if out.CodeDeliveryDetails == nil {
		return &port.CodeDeliveryDetails{}, nil
	}
	return &port.CodeDeliveryDetails{
		Destination:    derefStr(out.CodeDeliveryDetails.Destination),
		DeliveryMedium: string(out.CodeDeliveryDetails.DeliveryMedium),
		AttributeName:  derefStr(out.CodeDeliveryDetails.AttributeName),
	}, nil
}

func (a *Adapter) ConfirmForgotPassword(ctx context.Context, email, verificationCode, newPassword string) error {
	_, err := a.c.ConfirmForgotPassword(ctx, email, verificationCode, newPassword)
	return err
}

func (a *Adapter) AdminCreateUser(ctx context.Context, email, tempPassword string) (string, error) {
	return a.c.AdminCreateUser(ctx, email, tempPassword)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
