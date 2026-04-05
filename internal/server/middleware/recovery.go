package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/siigofiscal/go_backend/internal/response"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				slog.Error("panic recovered",
					"error", fmt.Sprint(rv),
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
				)
				response.InternalError(w, fmt.Sprint(rv))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
