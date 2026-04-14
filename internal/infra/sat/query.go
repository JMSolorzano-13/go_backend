package sat

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// soapFechaCentro formats FechaInicial/FechaFinal for SAT Descarga Masiva (Centro).
// Stored times use UTC-midnight calendar labels (MXCalendarDate / admin range); use UTC
// calendar components here so "2026-04-13Z" stays Apr 13 (In(Mexico).Date would be Apr 12).
// SAT expects that Y-M-D at 00:00:00 in America/Mexico_City.
func soapFechaCentro(t time.Time) string {
	loc, err := time.LoadLocation("America/Mexico_City")
	if err != nil {
		loc = time.UTC
	}
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).Format("2006-01-02T15:04:05")
}

// DefaultTimeWindow is the default query window when no start date is given (30 days).
const DefaultTimeWindow = 30 * 24 * time.Hour

// Query represents a SAT download request that progresses through send → verify → download.
// Matches Python Query class.
type Query struct {
	DownloadType DownloadType
	RequestType  RequestType
	Start        time.Time
	End          time.Time

	// Identifier is the SAT-assigned solicitud ID returned by send.
	Identifier string

	// Status fields populated by send/verify.
	Status     int
	CodEstatus string

	// Verify-specific fields.
	QueryStatus VerifyQueryStatus
	Message     string
	StatusCode  int
	CfdiQty     int
	Packages    []*Package

	SentDate     time.Time
	VerifiedDate time.Time
}

// NewQuery creates a Query with the given parameters.
// Matches Python Query.__init__.
func NewQuery(downloadType DownloadType, requestType RequestType, start, end time.Time) *Query {
	if end.IsZero() {
		end = time.Now()
	}
	if start.IsZero() {
		start = end.Add(-DefaultTimeWindow)
	}
	return &Query{
		DownloadType: downloadType,
		RequestType:  requestType,
		Start:        start,
		End:          end,
	}
}

// NewQueryFromIdentifier creates a Query for verify/download using an existing solicitud ID.
func NewQueryFromIdentifier(identifier string, requestType RequestType) *Query {
	return &Query{
		Identifier:  identifier,
		RequestType: requestType,
	}
}

// Send sends the solicitud request to SAT. Matches Python Query.send.
func (q *Query) Send(c *Connector) error {
	if q.DownloadType == "" || q.RequestType == "" {
		return fmt.Errorf("download_type and request_type must be set")
	}

	envelope, err := q.buildSendEnvelope(c)
	if err != nil {
		return fmt.Errorf("build send envelope: %w", err)
	}

	operation := q.operationName()
	resp, err := c.sendQuery(envelope, operation)
	if err != nil {
		return err
	}

	return q.processSendResponse(resp)
}

// Verify checks the status of a previously sent solicitud. Matches Python Query.verify.
func (q *Query) Verify(c *Connector) error {
	data := map[string]string{
		"identifier": q.Identifier,
		"signature":  "{signature}",
	}

	resp, err := c.verifyQuery(data)
	if err != nil {
		return err
	}

	return q.processVerifyResponse(resp)
}

// GetPackages polls verify until the query reaches a terminal state, then returns packages.
// Matches Python Query.get_packages.
func (q *Query) GetPackages(c *Connector, retries int, waitSeconds int) ([]*Package, error) {
	if retries <= 0 {
		retries = 10
	}
	if waitSeconds <= 0 {
		waitSeconds = 2
	}

	for i := 0; i < retries; i++ {
		if err := q.Verify(c); err != nil {
			return nil, err
		}

		if q.QueryStatus > VerifyStatusFinished {
			return nil, &QueryError{
				StatusCode: q.StatusCode,
				Message:    q.Message,
			}
		}

		if q.QueryStatus == VerifyStatusFinished {
			return q.Packages, nil
		}

		slog.Debug("sat: query not ready, waiting",
			"identifier", q.Identifier,
			"status", q.QueryStatus,
			"attempt", i+1,
		)
		time.Sleep(time.Duration(waitSeconds) * time.Second)
	}

	return nil, fmt.Errorf("timeout: query %s not resolved after %d retries", q.Identifier, retries)
}

// Download downloads all packages in this query. Matches Python Query.download.
func (q *Query) Download(c *Connector, process bool) error {
	for _, pkg := range q.Packages {
		if err := pkg.Download(c, process); err != nil {
			return fmt.Errorf("download package %s: %w", pkg.Identifier, err)
		}
	}
	return nil
}

// buildSendEnvelope constructs the SOAP envelope for the solicitud request.
func (q *Query) buildSendEnvelope(c *Connector) (string, error) {
	data := q.soapSendData()
	return c.getEnvelopeQuery(data)
}

// soapSendData builds the template data for the send request.
// Matches Python Query.soap_send.
func (q *Query) soapSendData() map[string]string {
	soapStart := soapFechaCentro(q.Start)
	soapEnd := soapFechaCentro(q.End)
	data := map[string]string{
		"start":         soapStart,
		"end":           soapEnd,
		"download_type": string(q.DownloadType),
		"request_type":  string(q.RequestType),
		"signature":     "{signature}",
	}

	// v1.5 for ISSUED/RECEIVED.
	if q.DownloadType == DownloadTypeIssued || q.DownloadType == DownloadTypeReceived {
		data["use_v15"] = "true"
	}

	return data
}

// operationName returns the SOAP operation name for the solicitud request.
// Matches Python Query._get_operation_name.
func (q *Query) operationName() string {
	switch q.DownloadType {
	case DownloadTypeIssued:
		return "SolicitaDescargaEmitidos"
	case DownloadTypeReceived:
		return "SolicitaDescargaRecibidos"
	case DownloadTypeFolio:
		return "SolicitaDescargaFolio"
	default:
		return "SolicitaDescarga"
	}
}

// processSendResponse parses the solicitud response.
func (q *Query) processSendResponse(resp *SOAPResponse) error {
	if err := checkResponse(resp); err != nil {
		return err
	}

	cleaned := removeNamespaces(string(resp.Body))
	parsed, err := parseQueryResponse([]byte(cleaned), q.DownloadType)
	if err != nil {
		return err
	}

	id := strings.TrimSpace(parsed.IdSolicitud)
	if id == "" {
		return fmt.Errorf("sat solicitud returned empty IdSolicitud (likely throttling or rejection; cod_estatus=%q)", parsed.CodEstatus)
	}

	q.CodEstatus = parsed.CodEstatus
	q.Identifier = id
	q.SentDate = time.Now()

	slog.Info("sat: solicitud sent",
		"identifier", q.Identifier,
		"cod_estatus", q.CodEstatus,
	)
	return nil
}

// processVerifyResponse parses the verify response.
func (q *Query) processVerifyResponse(resp *SOAPResponse) error {
	if err := checkResponse(resp); err != nil {
		return err
	}

	parsed, err := parseVerifyResponse(resp.Body)
	if err != nil {
		return err
	}

	q.CodEstatus = parsed.CodEstatus
	q.QueryStatus = VerifyQueryStatus(parsed.EstadoSolicitud)
	q.Message = parsed.Mensaje
	q.StatusCode = parsed.CodigoEstadoSolicitud
	q.CfdiQty = parsed.NumeroCFDIs

	q.Packages = make([]*Package, 0, len(parsed.IdsPaquetes))
	for _, id := range parsed.IdsPaquetes {
		q.Packages = append(q.Packages, NewPackage(id, q.RequestType))
	}

	q.VerifiedDate = time.Now()

	slog.Info("sat: verify result",
		"identifier", q.Identifier,
		"estado_solicitud", q.QueryStatus,
		"mensaje", q.Message,
		"num_cfdis", q.CfdiQty,
		"num_packages", len(q.Packages),
	)
	return nil
}
