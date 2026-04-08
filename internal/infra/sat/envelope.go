package sat

import (
	"crypto/sha1"
	"encoding/base64"
	"strings"
)

// templateReplace replaces {key} placeholders in a template string with values from data.
// Matches Python utils.prepare_template.
func templateReplace(template string, data map[string]string) string {
	result := strings.TrimSpace(template)
	for k, v := range data {
		result = strings.ReplaceAll(result, "{"+k+"}", v)
	}
	return result
}

// digest computes SHA1 hash of data and returns it as base64. Matches Python utils.digest.
func digest(data string) string {
	h := sha1.Sum([]byte(data))
	return base64.StdEncoding.EncodeToString(h[:])
}

// EnvelopeSigner creates signed SOAP envelopes for SAT web service operations.
// It implements the two-pass signing process:
//  1. Render template with empty signature to get the digest target.
//  2. Create SignedInfo with the digest.
//  3. Sign the SignedInfo.
//  4. Assemble the full Signature element and insert it into the envelope.
type EnvelopeSigner struct {
	ch *CertificateHandler
}

// NewEnvelopeSigner creates an EnvelopeSigner with the given certificate handler.
func NewEnvelopeSigner(ch *CertificateHandler) *EnvelopeSigner {
	return &EnvelopeSigner{ch: ch}
}

// CreateCommonEnvelope builds a signed SOAP envelope for authenticated operations
// (solicitud, verifica, descarga).
// Matches Python EnvelopeSigner.create_common_envelope.
func (es *EnvelopeSigner) CreateCommonEnvelope(template string, data map[string]string) (string, error) {
	// Step 1: Render template with the provided data (signature placeholder kept for re-insertion).
	queryDataSignature := templateReplace(template, data)

	// Render again with empty signature to compute the digest.
	dataForDigest := copyMap(data)
	dataForDigest["signature"] = ""
	queryData := templateReplace(template, dataForDigest)

	// Step 2: Compute digest of the unsigned content.
	digestValue := digest(queryData)

	// Step 3: Build SignedInfo.
	signedInfo := templateReplace(tplSignedInfo, map[string]string{
		"uri":          "",
		"digest_value": digestValue,
	})

	// Step 4: Build KeyInfo.
	keyInfo := templateReplace(tplKeyInfo, map[string]string{
		"issuer_name":   es.ch.Issuer,
		"serial_number": es.ch.SerialNumber,
		"certificate":   es.ch.CertBase64(),
	})

	// Step 5: Sign the SignedInfo.
	signatureValue, err := es.ch.Sign(signedInfo)
	if err != nil {
		return "", err
	}

	// Step 6: Assemble the Signature element.
	signature := templateReplace(tplSignature, map[string]string{
		"signed_info":     signedInfo,
		"signature_value": signatureValue,
		"key_info":        keyInfo,
	})

	// Step 7: Insert the signature into the query content.
	envelopeContent := templateReplace(queryDataSignature, map[string]string{
		"signature": signature,
	})

	// Step 8: Wrap in SOAP envelope.
	envelope := templateReplace(tplEnvelope, map[string]string{
		"content": envelopeContent,
	})

	return envelope, nil
}

func copyMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
