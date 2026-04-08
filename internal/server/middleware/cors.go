package middleware

import "net/http"

func corsOriginSet(allowedOrigins []string) map[string]bool {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o != "" {
			originSet[o] = true
		}
	}
	return originSet
}

// applyCORSHeaders mirrors browser CORS expectations for API + SPA (incl. custom headers).
func applyCORSHeaders(w http.ResponseWriter, r *http.Request, originSet map[string]bool) {
	origin := r.Header.Get("Origin")
	if originSet[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, access_token, authorization, company_identifier")
	w.Header().Set("Access-Control-Expose-Headers", "*")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.Header().Set("Vary", "Origin")
}

func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := corsOriginSet(allowedOrigins)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			applyCORSHeaders(w, r, originSet)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Preflight is registered on the root ServeMux as OPTIONS /api/{path...} so preflight
// requests get 204 + CORS headers even when the outer middleware chain does not handle
// OPTIONS (e.g. some Azure revisions / older images where CORS never wrapped the mux).
func Preflight(allowedOrigins []string) http.HandlerFunc {
	originSet := corsOriginSet(allowedOrigins)
	return func(w http.ResponseWriter, r *http.Request) {
		applyCORSHeaders(w, r, originSet)
		w.WriteHeader(http.StatusNoContent)
	}
}
