package sat

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// removeNamespaces strips SOAP namespace prefixes for simpler parsing.
// Matches Python utils.remove_namespaces: re.sub(r"[souh]:", "", xml)
var nsRegexp = regexp.MustCompile(`[souh]:`)

func removeNamespaces(xmlStr string) string {
	return nsRegexp.ReplaceAllString(xmlStr, "")
}

// --- Login response ---

// loginResponse is the parsed structure from the SAT authentication response.
type loginResponse struct {
	Token   string
	Created string
	Expires string
}

// parseLoginResponse extracts the token from the login SOAP response.
// Matches Python LoginParser.parse.
func parseLoginResponse(rawXML []byte) (*loginResponse, error) {
	cleaned := removeNamespaces(string(rawXML))

	// Parse the cleaned XML to extract the token.
	type timestamp struct {
		Created string `xml:"Created"`
		Expires string `xml:"Expires"`
	}
	type security struct {
		Timestamp timestamp `xml:"Timestamp"`
	}
	type header struct {
		Security security `xml:"Security"`
	}
	type autenticaResponse struct {
		AutenticaResult string `xml:"AutenticaResult"`
	}
	type body struct {
		AutenticaResponse autenticaResponse `xml:"AutenticaResponse"`
	}
	type envelope struct {
		Header header `xml:"Header"`
		Body   body   `xml:"Body"`
	}

	var env envelope
	if err := xml.Unmarshal([]byte(cleaned), &env); err != nil {
		return nil, fmt.Errorf("parse login response: %w", err)
	}

	if env.Body.AutenticaResponse.AutenticaResult == "" {
		return nil, fmt.Errorf("empty token in login response")
	}

	return &loginResponse{
		Token:   env.Body.AutenticaResponse.AutenticaResult,
		Created: env.Header.Security.Timestamp.Created,
		Expires: env.Header.Security.Timestamp.Expires,
	}, nil
}

// --- Query (Solicitud) response ---

// queryResponse holds the parsed solicitud result.
type queryResponse struct {
	CodEstatus  string
	IdSolicitud string
}

// parseQueryResponse extracts CodEstatus and IdSolicitud from the solicitud response.
// Matches Python QueryParser.parse — must handle different response element names
// depending on download type.
func parseQueryResponse(rawXML []byte, downloadType DownloadType) (*queryResponse, error) {
	cleaned := removeNamespaces(string(rawXML))

	responseName, resultName := queryResponseKeys(downloadType)

	// Use a generic approach: find the result element via tag name.
	type resultAttrs struct {
		CodEstatus  string `xml:"CodEstatus,attr"`
		IdSolicitud string `xml:"IdSolicitud,attr"`
	}

	// We need to find the specific nested element. Since Go xml doesn't support
	// dynamic tag names easily, we search the cleaned XML string.
	result, err := extractXMLAttrs(cleaned, responseName, resultName)
	if err != nil {
		return nil, fmt.Errorf("parse query response: %w", err)
	}

	return &queryResponse{
		CodEstatus:  result["CodEstatus"],
		IdSolicitud: result["IdSolicitud"],
	}, nil
}

// queryResponseKeys returns the element names for the query response based on download type.
func queryResponseKeys(dt DownloadType) (responseName, resultName string) {
	switch dt {
	case DownloadTypeIssued:
		return "SolicitaDescargaEmitidosResponse", "SolicitaDescargaEmitidosResult"
	case DownloadTypeReceived:
		return "SolicitaDescargaRecibidosResponse", "SolicitaDescargaRecibidosResult"
	case DownloadTypeFolio:
		return "SolicitaDescargaFolioResponse", "SolicitaDescargaFolioResult"
	default:
		return "SolicitaDescargaResponse", "SolicitaDescargaResult"
	}
}

// --- Verify response ---

// verifyResponse holds the parsed verification result.
type verifyResponse struct {
	CodEstatus            string
	EstadoSolicitud       int
	Mensaje               string
	CodigoEstadoSolicitud int
	NumeroCFDIs           int
	IdsPaquetes           []string
}

// parseVerifyResponse extracts the verification status from the SOAP response.
// Matches Python VerifyParser.parse.
func parseVerifyResponse(rawXML []byte) (*verifyResponse, error) {
	cleaned := removeNamespaces(string(rawXML))

	// Extract attributes from VerificaSolicitudDescargaResult
	attrs, err := extractXMLAttrs(cleaned,
		"VerificaSolicitudDescargaResponse", "VerificaSolicitudDescargaResult")
	if err != nil {
		return nil, fmt.Errorf("parse verify response: %w", err)
	}

	estadoSolicitud, _ := strconv.Atoi(attrs["EstadoSolicitud"])
	codEstatus := attrs["CodEstatus"]
	mensaje := attrs["Mensaje"]
	codigoEstadoSolicitud, _ := strconv.Atoi(attrs["CodigoEstadoSolicitud"])
	numeroCFDIs, _ := strconv.Atoi(attrs["NumeroCFDIs"])

	// Parse package IDs — only when EstadoSolicitud == 3 (FINISHED).
	var ids []string
	if estadoSolicitud == 3 {
		ids = extractPackageIDs(cleaned)
	}

	return &verifyResponse{
		CodEstatus:            codEstatus,
		EstadoSolicitud:       estadoSolicitud,
		Mensaje:               mensaje,
		CodigoEstadoSolicitud: codigoEstadoSolicitud,
		NumeroCFDIs:           numeroCFDIs,
		IdsPaquetes:           ids,
	}, nil
}

// --- Download response ---

// downloadResponse holds the parsed download result.
type downloadResponse struct {
	CodEstatus int
	Content    string // Base64-encoded ZIP content
}

// parseDownloadResponse extracts the package content from the download SOAP response.
// Matches Python DownloadParser.parse.
func parseDownloadResponse(rawXML []byte) (*downloadResponse, error) {
	cleaned := removeNamespaces(string(rawXML))

	// Extract CodEstatus from Header/respuesta
	headerAttrs := extractAttrsFromTag(cleaned, "respuesta")
	codEstatus, _ := strconv.Atoi(headerAttrs["CodEstatus"])

	// Extract Paquete content from Body
	content := extractTagContent(cleaned, "Paquete")

	return &downloadResponse{
		CodEstatus: codEstatus,
		Content:    content,
	}, nil
}

// --- XML utility helpers ---

// extractXMLAttrs finds a result element inside a response element and extracts its XML attributes.
// This handles the SAT response pattern where data is in attributes like @CodEstatus.
func extractXMLAttrs(xmlStr, responseName, resultName string) (map[string]string, error) {
	// Find the result element opening tag.
	idx := strings.Index(xmlStr, resultName)
	if idx == -1 {
		return nil, fmt.Errorf("element %q not found in response", resultName)
	}

	// Find the tag boundaries after the element name.
	tagStart := strings.LastIndex(xmlStr[:idx], "<")
	if tagStart == -1 {
		return nil, fmt.Errorf("malformed XML around %q", resultName)
	}

	// Find the end of the opening tag.
	tagEnd := strings.Index(xmlStr[tagStart:], ">")
	if tagEnd == -1 {
		return nil, fmt.Errorf("unclosed tag for %q", resultName)
	}

	tag := xmlStr[tagStart : tagStart+tagEnd+1]
	return parseTagAttributes(tag), nil
}

// parseTagAttributes extracts name="value" pairs from an XML opening tag string.
func parseTagAttributes(tag string) map[string]string {
	attrs := make(map[string]string)
	// Match attr="value" patterns.
	re := regexp.MustCompile(`(\w+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(tag, -1)
	for _, m := range matches {
		attrs[m[1]] = m[2]
	}
	return attrs
}

// extractPackageIDs finds all <IdsPaquetes>...</IdsPaquetes> values in the XML.
func extractPackageIDs(xmlStr string) []string {
	var ids []string
	re := regexp.MustCompile(`<IdsPaquetes>([^<]+)</IdsPaquetes>`)
	matches := re.FindAllStringSubmatch(xmlStr, -1)
	for _, m := range matches {
		id := strings.TrimSpace(m[1])
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// extractAttrsFromTag finds the first occurrence of <tagName ...> and extracts its attributes.
func extractAttrsFromTag(xmlStr, tagName string) map[string]string {
	re := regexp.MustCompile(`<` + regexp.QuoteMeta(tagName) + `\s+([^>]*)>`)
	m := re.FindStringSubmatch(xmlStr)
	if m == nil {
		return nil
	}
	return parseTagAttributes("<" + tagName + " " + m[1] + ">")
}

// extractTagContent extracts the text content of the first <tagName>...</tagName>.
func extractTagContent(xmlStr, tagName string) string {
	open := "<" + tagName + ">"
	close := "</" + tagName + ">"
	start := strings.Index(xmlStr, open)
	if start == -1 {
		return ""
	}
	start += len(open)
	end := strings.Index(xmlStr[start:], close)
	if end == -1 {
		return ""
	}
	return xmlStr[start : start+end]
}
