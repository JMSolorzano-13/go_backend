package db

import "net/http"

// InjectDatabase attaches the Database to every request context so handlers
// can retrieve it via db.FromContext / db.ControlDB.
func InjectDatabase(database *Database) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithDatabase(r.Context(), database)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
