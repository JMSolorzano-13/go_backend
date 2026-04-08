package sat

import (
	"fmt"
	"log/slog"
	"time"
)

// Connector is the main SAT web service client, combining authentication,
// envelope signing, and SOAP operations for the descarga masiva service.
// Matches Python SATConnector.
type Connector struct {
	RFC string

	loginHandler   *LoginHandler
	envelopeSigner *EnvelopeSigner
	timeout        time.Duration
}

// NewConnector creates a SAT connector from raw DER certificate, encrypted key, and password.
// Matches Python SATConnector.__init__.
func NewConnector(certDER, keyDER, password []byte, timeout time.Duration) (*Connector, error) {
	ch, err := NewCertificateHandler(certDER, keyDER, password)
	if err != nil {
		return nil, fmt.Errorf("load SAT credentials: %w", err)
	}

	rfc := HandleSpecialCharactersInRFC(EscapeXML(ch.RFC))

	if timeout == 0 {
		timeout = DefaultTimeout
	}

	slog.Info("sat: connector initialized", "rfc", rfc)

	return &Connector{
		RFC:            rfc,
		loginHandler:   NewLoginHandler(ch, timeout),
		envelopeSigner: NewEnvelopeSigner(ch),
		timeout:        timeout,
	}, nil
}

// getEnvelopeQuery builds the SOAP envelope for a solicitud request,
// handling v1.4 vs v1.5 format selection.
// Matches Python SATConnector.get_envelope_query.
func (c *Connector) getEnvelopeQuery(data map[string]string) (string, error) {
	if data["use_v15"] == "true" {
		return c.getEnvelopeQueryV15(data)
	}
	return c.getEnvelopeQueryV14(data)
}

// getEnvelopeQueryV14 builds a v1.4 legacy query envelope.
// Matches Python SATConnector._get_envelope_query_v14.
func (c *Connector) getEnvelopeQueryV14(data map[string]string) (string, error) {
	downloadType := data["download_type"]

	rfc_issued := ""
	rfc_received := ""
	if downloadType == string(DownloadTypeIssued) {
		rfc_issued = fmt.Sprintf(` RfcEmisor="%s"`, c.RFC)
	} else if downloadType == string(DownloadTypeReceived) {
		rfc_received = fmt.Sprintf(
			"<des:RfcReceptores><des:RfcReceptor>%s</des:RfcReceptor></des:RfcReceptores>",
			c.RFC,
		)
	}

	data["rfc_issued"] = rfc_issued
	data["rfc_received"] = rfc_received
	data["rfc"] = c.RFC

	return c.envelopeSigner.CreateCommonEnvelope(tplSolicitaDescarga, data)
}

// getEnvelopeQueryV15 builds a v1.5 query envelope with separate operations.
// Matches Python SATConnector._get_envelope_query_v15.
func (c *Connector) getEnvelopeQueryV15(data map[string]string) (string, error) {
	downloadType := data["download_type"]

	var template string
	switch DownloadType(downloadType) {
	case DownloadTypeIssued:
		template = tplSolicitaDescargaEmitidos
	case DownloadTypeReceived:
		template = tplSolicitaDescargaRecibidos
	default:
		return "", fmt.Errorf("v1.5 format not supported for download type: %s", downloadType)
	}

	queryData := c.prepareV15QueryData(data)
	return c.envelopeSigner.CreateCommonEnvelope(template, queryData)
}

// prepareV15QueryData prepares data for v1.5 query operations.
// Matches Python SATConnector._prepare_v15_query_data.
func (c *Connector) prepareV15QueryData(data map[string]string) map[string]string {
	result := copyMap(data)
	result["rfc"] = c.RFC

	// CFDIs Recibidos solo se pueden descargar si están Vigentes.
	canCancelled := !(data["download_type"] == string(DownloadTypeReceived) &&
		data["request_type"] == string(RequestTypeCFDI))

	estadoComprobante := string(EstadoComprobanteTodos)
	if !canCancelled {
		estadoComprobante = string(EstadoComprobanteVigente)
	}

	result["complemento"] = ""
	result["estado_comprobante"] = estadoComprobante
	result["tipo_comprobante"] = ""
	result["rfc_a_cuenta_terceros"] = ""
	result["rfc_solicitante"] = c.RFC

	if DownloadType(data["download_type"]) == DownloadTypeIssued {
		result["rfc_receptor"] = ""
	} else if DownloadType(data["download_type"]) == DownloadTypeReceived {
		result["rfc_receptor"] = fmt.Sprintf(` RfcReceptor="%s"`, c.RFC)
	}

	return result
}

// sendQuery sends a solicitud request to the SAT web service.
// Matches Python SATConnector.send_query.
func (c *Connector) sendQuery(envelope string, operation string) (*SOAPResponse, error) {
	if operation == "" {
		operation = "SolicitaDescarga"
	}

	token, err := c.loginHandler.Token()
	if err != nil {
		return nil, fmt.Errorf("get token for send_query: %w", err)
	}

	soapAction := soapActionSolicita + operation

	slog.Debug("sat: sending solicitud", "operation", operation)

	return soapConsume(soapAction, urlSolicitaDescarga, envelope, token, c.timeout)
}

// verifyQuery sends a verification request for a pending solicitud.
// Matches Python SATConnector.verify_query.
func (c *Connector) verifyQuery(data map[string]string) (*SOAPResponse, error) {
	data["rfc"] = c.RFC

	envelope, err := c.envelopeSigner.CreateCommonEnvelope(tplVerificaSolicitudDescarga, data)
	if err != nil {
		return nil, fmt.Errorf("build verify envelope: %w", err)
	}

	token, err := c.loginHandler.Token()
	if err != nil {
		return nil, fmt.Errorf("get token for verify: %w", err)
	}

	return soapConsume(soapActionVerifica, urlVerificaSolicitud, envelope, token, c.timeout)
}

// downloadPackage fetches a package from the SAT descarga masiva service.
// Matches Python SATConnector.download_package.
func (c *Connector) downloadPackage(data map[string]string) (*SOAPResponse, error) {
	data["rfc"] = c.RFC

	envelope, err := c.envelopeSigner.CreateCommonEnvelope(tplPeticionDescarga, data)
	if err != nil {
		return nil, fmt.Errorf("build download envelope: %w", err)
	}

	token, err := c.loginHandler.Token()
	if err != nil {
		return nil, fmt.Errorf("get token for download: %w", err)
	}

	return soapConsume(soapActionDescarga, urlDescargaMasiva, envelope, token, c.timeout)
}

// SendQueryRequest is a convenience method that creates a Query, sends it, and returns
// the populated Query. Matches the common Python pattern of creating + sending a Query.
func (c *Connector) SendQueryRequest(downloadType DownloadType, requestType RequestType, start, end time.Time) (*Query, error) {
	q := NewQuery(downloadType, requestType, start, end)
	if err := q.Send(c); err != nil {
		return nil, err
	}
	return q, nil
}

// VerifyRequest verifies a previously sent solicitud by its identifier.
func (c *Connector) VerifyRequest(identifier string, requestType RequestType) (*Query, error) {
	q := NewQueryFromIdentifier(identifier, requestType)
	if err := q.Verify(c); err != nil {
		return nil, err
	}
	return q, nil
}

// DownloadPackage downloads a single package by ID.
func (c *Connector) DownloadPackage(packageID string, requestType RequestType) (*Package, error) {
	pkg := NewPackage(packageID, requestType)
	if err := pkg.Download(c, true); err != nil {
		return nil, err
	}
	return pkg, nil
}

