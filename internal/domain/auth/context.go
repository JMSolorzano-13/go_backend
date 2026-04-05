package auth

import (
	"context"

	"github.com/siigofiscal/go_backend/internal/model/control"
)

type ctxKey int

const (
	ctxKeyUser ctxKey = iota
	ctxKeyCompanyIdentifier
	ctxKeyCompany
	ctxKeyJSONBody
)

func WithUser(ctx context.Context, user *control.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, user)
}

func UserFromContext(ctx context.Context) (*control.User, bool) {
	user, ok := ctx.Value(ctxKeyUser).(*control.User)
	return user, ok
}

func WithCompanyIdentifier(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyCompanyIdentifier, id)
}

func CompanyIdentifierFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKeyCompanyIdentifier).(string)
	return id, ok
}

func WithCompany(ctx context.Context, company *control.Company) context.Context {
	return context.WithValue(ctx, ctxKeyCompany, company)
}

func CompanyFromContext(ctx context.Context) (*control.Company, bool) {
	company, ok := ctx.Value(ctxKeyCompany).(*control.Company)
	return company, ok
}

func WithJSONBody(ctx context.Context, body map[string]interface{}) context.Context {
	return context.WithValue(ctx, ctxKeyJSONBody, body)
}

func JSONBodyFromContext(ctx context.Context) map[string]interface{} {
	body, _ := ctx.Value(ctxKeyJSONBody).(map[string]interface{})
	return body
}
