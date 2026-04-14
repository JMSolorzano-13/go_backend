package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/siigofiscal/go_backend/internal/config"
	"github.com/siigofiscal/go_backend/internal/domain/port"
)

type stubIDP struct {
	signUpSub string
	signUpErr error
}

func (s *stubIDP) InitiateAuth(context.Context, string, map[string]string) (*port.InitiateAuthResult, error) {
	return nil, errors.New("not implemented")
}
func (s *stubIDP) RespondToAuthChallenge(context.Context, string, string, string, string) (*port.RespondChallengeResult, error) {
	return nil, errors.New("not implemented")
}
func (s *stubIDP) SignUp(_ context.Context, _, _ string) (string, error) {
	if s.signUpErr != nil {
		return "", s.signUpErr
	}
	return s.signUpSub, nil
}
func (s *stubIDP) ChangePassword(context.Context, string, string, string) error {
	return errors.New("not implemented")
}
func (s *stubIDP) ForgotPassword(context.Context, string) (*port.CodeDeliveryDetails, error) {
	return nil, errors.New("not implemented")
}
func (s *stubIDP) ConfirmForgotPassword(context.Context, string, string, string) error {
	return errors.New("not implemented")
}
func (s *stubIDP) AdminCreateUser(context.Context, string, string) (string, error) {
	return "", errors.New("not implemented")
}

func TestSignupIdentitySourceTag_AzureBeforeLocalInfra(t *testing.T) {
	cfg := &config.Config{
		CloudProvider:  "azure",
		LocalInfra:     true,
		AWSEndpointURL: "http://localhost:4566",
	}
	if g := signupIdentitySourceTag(cfg); g != "azure_selfauth" {
		t.Fatalf("want azure_selfauth, got %q", g)
	}
}

func TestResolveCreateUserIdentity_AzureWithLocalInfraSetsPassword(t *testing.T) {
	cfg := &config.Config{
		CloudProvider:  "azure",
		LocalInfra:     true,
		AWSEndpointURL: "http://localhost:4566",
	}
	idp := &stubIDP{signUpErr: errors.New("cognito must not be called")}
	sub, hash, err := resolveCreateUserIdentity(context.Background(), cfg, idp, "a@b.com", "Test123.")
	if err != nil {
		t.Fatal(err)
	}
	if hash == nil || *hash == "" {
		t.Fatal("expected bcrypt password hash for azure path")
	}
	if sub == "" {
		t.Fatal("expected cognito_sub")
	}
}

func TestResolveCreateUserIdentity_LocalStackWithoutLocalInfraSkipsCognito(t *testing.T) {
	cfg := &config.Config{
		CloudProvider:  "aws",
		LocalInfra:     false,
		AWSEndpointURL: "http://localhost:4566",
	}
	idp := &stubIDP{signUpErr: errors.New("cognito must not be called")}
	sub, hash, err := resolveCreateUserIdentity(context.Background(), cfg, idp, "x@y.com", "Test123.")
	if err != nil {
		t.Fatal(err)
	}
	if hash != nil {
		t.Fatalf("expected nil password hash, got non-nil")
	}
	if want := "local-signup-x-y.com"; sub != want {
		t.Fatalf("sub: want %q, got %q", want, sub)
	}
}

func TestResolveCreateUserIdentity_AwsProdUsesIDP(t *testing.T) {
	cfg := &config.Config{
		CloudProvider:  "aws",
		LocalInfra:     false,
		AWSEndpointURL: "",
	}
	idp := &stubIDP{signUpSub: "cognito-uuid"}
	sub, hash, err := resolveCreateUserIdentity(context.Background(), cfg, idp, "x@y.com", "Test123.")
	if err != nil {
		t.Fatal(err)
	}
	if hash != nil {
		t.Fatal("expected nil hash from cognito path")
	}
	if sub != "cognito-uuid" {
		t.Fatalf("got %q", sub)
	}
}
