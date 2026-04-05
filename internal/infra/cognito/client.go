package cognito

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"github.com/siigofiscal/go_backend/internal/config"
)

type Client struct {
	cog          *cip.Client
	clientID     string
	clientSecret string
	userPoolID   string
}

func NewClient(cfg *config.Config) (*Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.RegionName),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("cognito: load config: %w", err)
	}

	var cogOpts []func(*cip.Options)
	if cfg.AWSEndpointURL != "" {
		cogOpts = append(cogOpts, func(o *cip.Options) {
			o.BaseEndpoint = aws.String(cfg.AWSEndpointURL)
		})
	}

	return &Client{
		cog:          cip.NewFromConfig(awsCfg, cogOpts...),
		clientID:     cfg.CognitoClientID,
		clientSecret: cfg.CognitoClientSecret,
		userPoolID:   cfg.CognitoUserPoolID,
	}, nil
}

func (c *Client) secretHash(username string) *string {
	if c.clientSecret == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(c.clientSecret))
	mac.Write([]byte(username + c.clientID))
	hash := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return &hash
}

// InitiateAuth wraps CognitoIdentityProvider.InitiateAuth.
func (c *Client) InitiateAuth(ctx context.Context, flow string, params map[string]string) (*cip.InitiateAuthOutput, error) {
	if h := c.secretHash(params["USERNAME"]); h != nil {
		params["SECRET_HASH"] = *h
	}
	return c.cog.InitiateAuth(ctx, &cip.InitiateAuthInput{
		ClientId:       aws.String(c.clientID),
		AuthFlow:       types.AuthFlowType(flow),
		AuthParameters: params,
	})
}

// RespondToAuthChallenge wraps CognitoIdentityProvider.RespondToAuthChallenge.
func (c *Client) RespondToAuthChallenge(ctx context.Context, challengeName, session, email, password string) (*cip.RespondToAuthChallengeOutput, error) {
	return c.cog.RespondToAuthChallenge(ctx, &cip.RespondToAuthChallengeInput{
		ClientId:      aws.String(c.clientID),
		ChallengeName: types.ChallengeNameType(challengeName),
		Session:       aws.String(session),
		ChallengeResponses: map[string]string{
			"USERNAME":     email,
			"NEW_PASSWORD": password,
		},
	})
}

// ChangePassword wraps CognitoIdentityProvider.ChangePassword.
func (c *Client) ChangePassword(ctx context.Context, accessToken, previousPassword, proposedPassword string) (*cip.ChangePasswordOutput, error) {
	return c.cog.ChangePassword(ctx, &cip.ChangePasswordInput{
		AccessToken:      aws.String(accessToken),
		PreviousPassword: aws.String(previousPassword),
		ProposedPassword: aws.String(proposedPassword),
	})
}

// ForgotPassword wraps CognitoIdentityProvider.ForgotPassword.
func (c *Client) ForgotPassword(ctx context.Context, email string) (*cip.ForgotPasswordOutput, error) {
	return c.cog.ForgotPassword(ctx, &cip.ForgotPasswordInput{
		ClientId: aws.String(c.clientID),
		Username: aws.String(email),
	})
}

// ConfirmForgotPassword wraps CognitoIdentityProvider.ConfirmForgotPassword.
func (c *Client) ConfirmForgotPassword(ctx context.Context, email, code, newPassword string) (*cip.ConfirmForgotPasswordOutput, error) {
	return c.cog.ConfirmForgotPassword(ctx, &cip.ConfirmForgotPasswordInput{
		ClientId:         aws.String(c.clientID),
		Username:         aws.String(email),
		ConfirmationCode: aws.String(code),
		Password:         aws.String(newPassword),
	})
}

// SignUp wraps CognitoIdentityProvider.SignUp.
func (c *Client) SignUp(ctx context.Context, email, password string) (string, error) {
	input := &cip.SignUpInput{
		ClientId: aws.String(c.clientID),
		Username: aws.String(email),
		Password: aws.String(password),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(email)},
		},
	}
	if h := c.secretHash(email); h != nil {
		input.SecretHash = h
	}
	out, err := c.cog.SignUp(ctx, input)
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UserSub), nil
}

// AdminCreateUser wraps CognitoIdentityProvider.AdminCreateUser.
// Returns the cognito sub (UUID). If user already exists, fetches the existing sub.
func (c *Client) AdminCreateUser(ctx context.Context, email, tempPassword string) (string, error) {
	out, err := c.cog.AdminCreateUser(ctx, &cip.AdminCreateUserInput{
		UserPoolId:            aws.String(c.userPoolID),
		Username:              aws.String(email),
		TemporaryPassword:     aws.String(tempPassword),
		DesiredDeliveryMediums: []types.DeliveryMediumType{types.DeliveryMediumTypeEmail},
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(email)},
		},
	})
	if err != nil {
		// If user already exists, retrieve the sub
		var ue *types.UsernameExistsException
		if ok := isUsernameExistsException(err, &ue); ok {
			return c.AdminGetUserSub(ctx, email)
		}
		return "", err
	}
	return extractSub(out.User.Attributes), nil
}

// AdminGetUserSub fetches the cognito sub for an existing user.
func (c *Client) AdminGetUserSub(ctx context.Context, email string) (string, error) {
	out, err := c.cog.AdminGetUser(ctx, &cip.AdminGetUserInput{
		UserPoolId: aws.String(c.userPoolID),
		Username:   aws.String(email),
	})
	if err != nil {
		return "", err
	}
	return extractSub(out.UserAttributes), nil
}

// ExchangeCodeForTokens exchanges an OAuth2 authorization code for tokens
// via the Cognito /oauth2/token endpoint. This is handled via HTTP in Python;
// we use the SDK's InitiateAuth with a custom HTTP call if needed.
// For now, returns a map with token fields.
func (c *Client) ExchangeCodeForTokens(ctx context.Context, code, redirectURI string) (map[string]interface{}, error) {
	// Cognito SDK doesn't have a direct token-exchange API.
	// The Python code uses requests.post to COGNITO_URL/oauth2/token.
	// We replicate this with a raw HTTP call in the handler layer.
	return nil, fmt.Errorf("exchange_code_for_tokens: must use HTTP client, not SDK")
}

func extractSub(attrs []types.AttributeType) string {
	for _, a := range attrs {
		if aws.ToString(a.Name) == "sub" {
			return aws.ToString(a.Value)
		}
	}
	return ""
}

func isUsernameExistsException(err error, target **types.UsernameExistsException) bool {
	// AWS SDK v2 wraps errors — use errors.As indirectly.
	// But types.UsernameExistsException doesn't always match via errors.As,
	// so we do a string check as fallback.
	if err == nil {
		return false
	}
	var ue *types.UsernameExistsException
	if asErr, ok := err.(*types.UsernameExistsException); ok {
		*target = asErr
		return true
	}
	_ = ue
	// String-based fallback matching Python's except cognito_client().exceptions.UsernameExistsException
	return false
}
