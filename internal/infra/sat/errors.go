package sat

import "fmt"

// RequestError is returned when the SAT web service responds with a non-200 status.
type RequestError struct {
	StatusCode int
	Reason     string
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("sat request failed: HTTP %d %s", e.StatusCode, e.Reason)
}

// QueryError is returned when a query is in an invalid or unexpected state.
type QueryError struct {
	StatusCode int
	Message    string
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("sat query error: EstadoSolicitud(%d) %s", e.StatusCode, e.Message)
}

// TooManyDownloadsError is returned when SAT responds with CodEstatus 5008
// (package was already downloaded twice).
type TooManyDownloadsError struct{}

func (e *TooManyDownloadsError) Error() string {
	return "no content downloaded: package may have already been downloaded twice (SAT error 5008)"
}

// CertsNotFoundError is returned when FIEL certificates are not found in S3.
type CertsNotFoundError struct {
	Detail string
}

func (e *CertsNotFoundError) Error() string {
	return fmt.Sprintf("certificates not found: %s", e.Detail)
}
