package handlers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uptrace/bun"

	tenant "github.com/siigofiscal/go_backend/internal/model/tenant"
)

// insertPayments parses the Complemento/Pagos element and creates Payment +
// DoctoRelacionado (payment_relation) rows. Only runs for TipoDeComprobante="P".
// After inserting, it updates cfdi.pr_count with the total DoctoRelacionado count.
//
// Mirrors Python XMLProcessor.generate_payments.
// Uses deterministic UUIDs for idempotent reprocessing.
// Errors are logged and never fail the parent CFDI processing.
func (h *ProcessXML) insertPayments(
	ctx context.Context,
	conn bun.Conn,
	data *cfdiXMLData,
	companyID, xmlContent string,
	now time.Time,
	logger *slog.Logger,
) {
	if data.TipoDeComprobante != "P" {
		return
	}

	pagos, err := parsePagosComplemento(xmlContent)
	if err != nil {
		logger.Debug("parse Pagos complemento failed", "uuid", data.UUID, "error", err)
		return
	}
	if len(pagos) == 0 {
		return
	}

	round6 := func(f float64) float64 { return math.Round(f*1e6) / 1e6 }

	totalDoctos := 0
	for idx, pago := range pagos {
		paymentID := uuid.NewSHA1(uuid.NameSpaceURL,
			[]byte(fmt.Sprintf("pay:%s:%s:%d", companyID, data.UUID, idx)),
		).String()

		now2 := now
		payment := &tenant.Payment{
			Identifier:        paymentID,
			CompanyIdentifier: companyID,
			IsIssued:          data.IsIssued,
			Estatus:           true,
			UUIDOrigin:        data.UUID,
			Index:             int64(idx),
			FechaPago:         pago.FechaPago,
			FormaDePagoP:      pago.FormaDePagoP,
			MonedaP:           pago.MonedaP,
			Monto:             pago.Monto,
			TipoCambioP:       nilIfZero(pago.TipoCambioP),
			NumOperacion:      nilIfEmpty(pago.NumOperacion),
			RfcEmisorCtaOrd:   nilIfEmpty(pago.RfcEmisorCtaOrd),
			NomBancoOrdExt:    nilIfEmpty(pago.NomBancoOrdExt),
			CtaOrdenante:      nilIfEmpty(pago.CtaOrdenante),
			RfcEmisorCtaBen:   nilIfEmpty(pago.RfcEmisorCtaBen),
			CtaBeneficiario:   nilIfEmpty(pago.CtaBeneficiario),
			TipoCadPago:       nilIfEmpty(pago.TipoCadPago),
			CertPago:          nilIfEmpty(pago.CertPago),
			CadPago:           nilIfEmpty(pago.CadPago),
			SelloPago:         nilIfEmpty(pago.SelloPago),
			CreatedAt:         now,
			UpdatedAt:         &now2,
		}

		if _, err := conn.NewInsert().
			Model(payment).
			On("CONFLICT (identifier, company_identifier) DO NOTHING").
			Exec(ctx); err != nil {
			logger.Error("insert payment failed", "uuid", data.UUID, "index", idx, "error", err)
			continue
		}

		for _, docto := range pago.DoctoRelacionados {
			uuidRelated := strings.ToLower(strings.TrimSpace(docto.IdDocumento))
			if uuidRelated == "" {
				continue
			}

			doctoID := uuid.NewSHA1(uuid.NameSpaceURL,
				[]byte(fmt.Sprintf("dr:%s:%s:%s:%d", companyID, paymentID, uuidRelated, docto.NumParcialidad)),
			).String()

			// Conversion rate: EquivalenciaDR takes precedence over TipoCambioP.
			tc := 1.0
			if docto.EquivalenciaDR > 0 {
				tc = docto.EquivalenciaDR
			} else if pago.TipoCambioP > 0 {
				tc = pago.TipoCambioP
			}

			var base16, base8, base0, baseExento float64
			var iva16, iva8, trasladosIVAMXN float64
			for _, t := range docto.TrasladosDR {
				if t.ImpuestoDR == "002" { // IVA
					switch {
					case t.TasaOCuotaDR >= 0.15 && t.TasaOCuotaDR <= 0.17: // 16%
						base16 += t.BaseDR
						iva16 += t.ImporteDR
					case t.TasaOCuotaDR >= 0.07 && t.TasaOCuotaDR <= 0.09: // 8%
						base8 += t.BaseDR
						iva8 += t.ImporteDR
					case t.TasaOCuotaDR == 0:
						base0 += t.BaseDR
					}
					if t.TipoFactorDR == "Exento" {
						baseExento += t.BaseDR
					}
					trasladosIVAMXN += t.ImporteDR * tc
				}
			}

			trasladosJSON, _ := json.Marshal(docto.TrasladosDR)
			retencionesJSON, _ := json.Marshal(docto.RetencionesDR)
			if trasladosJSON == nil {
				trasladosJSON = []byte("[]")
			}
			if retencionesJSON == nil {
				retencionesJSON = []byte("[]")
			}

			dr := &tenant.DoctoRelacionado{
				Identifier:        doctoID,
				CompanyIdentifier: companyID,
				IsIssued:          data.IsIssued,
				PaymentIdentifier: paymentID,
				UUID:              data.UUID,
				FechaPago:         pago.FechaPago,
				UUIDRelated:       uuidRelated,
				Serie:             nilIfEmpty(docto.Serie),
				Folio:             nilIfEmpty(docto.Folio),
				MonedaDR:          docto.MonedaDR,
				EquivalenciaDR:    nilIfZero(docto.EquivalenciaDR),
				MetodoDePagoDR:    nilIfEmpty(docto.MetodoDePagoDR),
				NumParcialidad:    docto.NumParcialidad,
				ImpSaldoAnt:       docto.ImpSaldoAnt,
				ImpPagado:         docto.ImpPagado,
				ImpPagadoMXN:      round6(docto.ImpPagado * tc),
				ImpSaldoInsoluto:  docto.ImpSaldoInsoluto,
				Active:            true,
				Applied:           false,
				ObjetoImpDR:       nilIfEmpty(docto.ObjetoImpDR),
				BaseIVA16:         round6(base16),
				BaseIVA8:          round6(base8),
				BaseIVA0:          round6(base0),
				BaseIVAExento:     round6(baseExento),
				IVATrasladado16:   round6(iva16),
				IVATrasladado8:    round6(iva8),
				TrasladosIVAMXN:   round6(trasladosIVAMXN),
				TrasladosDR:       trasladosJSON,
				RetencionesDR:     retencionesJSON,
				Estatus:           true,
				ExcludeFromIVA:    false,
				ExcludeFromISR:    false,
				CreatedAt:         &now,
			}

			if _, err := conn.NewInsert().
				Model(dr).
				On("CONFLICT (identifier, company_identifier) DO NOTHING").
				Exec(ctx); err != nil {
				logger.Error("insert docto_relacionado failed",
					"uuid", data.UUID, "payment_id", paymentID, "error", err)
				continue
			}
			totalDoctos++
		}
	}

	// Update pr_count on the CFDI row (mirrors Python's pr_count update).
	if totalDoctos > 0 {
		if _, err := conn.NewUpdate().
			Model((*tenant.CFDI)(nil)).
			Set("pr_count = ?", float64(totalDoctos)).
			Where("company_identifier = ?", companyID).
			Where(`"UUID" = ?`, data.UUID).
			Exec(ctx); err != nil {
			logger.Error("update pr_count failed", "uuid", data.UUID, "error", err)
		}
	}
}

// ─── Pagos parsing ───────────────────────────────────────────────────────────

// pagoParsed holds one parsed Pagos/Pago element.
type pagoParsed struct {
	FechaPago         time.Time
	FormaDePagoP      string
	MonedaP           string
	Monto             float64
	TipoCambioP       float64
	NumOperacion      string
	RfcEmisorCtaOrd   string
	NomBancoOrdExt    string
	CtaOrdenante      string
	RfcEmisorCtaBen   string
	CtaBeneficiario   string
	TipoCadPago       string
	CertPago          string
	CadPago           string
	SelloPago         string
	DoctoRelacionados []doctoRelParsed
}

// doctoRelParsed holds one parsed DoctoRelacionado inside a Pago.
type doctoRelParsed struct {
	IdDocumento      string
	Serie            string
	Folio            string
	MonedaDR         string
	EquivalenciaDR   float64
	MetodoDePagoDR   string
	NumParcialidad   int64
	ImpSaldoAnt      float64
	ImpPagado        float64
	ImpSaldoInsoluto float64
	ObjetoImpDR      string
	TrasladosDR      []trasladoDRParsed
	RetencionesDR    []retencionDRParsed
}

// trasladoDRParsed is serialized directly as JSONB into the payment_relation row.
// Values are stored as numbers (Go JSON default), compatible with Azure-only usage.
type trasladoDRParsed struct {
	BaseDR       float64 `json:"BaseDR"`
	ImpuestoDR   string  `json:"ImpuestoDR"`
	TipoFactorDR string  `json:"TipoFactorDR"`
	TasaOCuotaDR float64 `json:"TasaOCuotaDR"`
	ImporteDR    float64 `json:"ImporteDR"`
}

// retencionDRParsed is serialized directly as JSONB into the payment_relation row.
type retencionDRParsed struct {
	ImpuestoDR string  `json:"ImpuestoDR"`
	ImporteDR  float64 `json:"ImporteDR"`
}

// parsePagosComplemento extracts all Pago elements from the Complemento/Pagos node.
// Handles both Pagos v1.0 (CFDI 3.3) and v2.0 (CFDI 4.0) — Go matches by local name.
func parsePagosComplemento(xmlContent string) ([]pagoParsed, error) {
	type TrasladoDR struct {
		BaseDR       string `xml:"BaseDR,attr"`
		ImpuestoDR   string `xml:"ImpuestoDR,attr"`
		TipoFactorDR string `xml:"TipoFactorDR,attr"`
		TasaOCuotaDR string `xml:"TasaOCuotaDR,attr"`
		ImporteDR    string `xml:"ImporteDR,attr"`
	}
	type RetencionDR struct {
		ImpuestoDR string `xml:"ImpuestoDR,attr"`
		ImporteDR  string `xml:"ImporteDR,attr"`
	}
	type TrasladosDR struct {
		Traslado []TrasladoDR `xml:"TrasladoDR"`
	}
	type RetencionesDR struct {
		Retencion []RetencionDR `xml:"RetencionDR"`
	}
	type ImpuestosDR struct {
		Traslados   *TrasladosDR   `xml:"TrasladosDR"`
		Retenciones *RetencionesDR `xml:"RetencionesDR"`
	}
	type DoctoRelacionado struct {
		IdDocumento      string       `xml:"IdDocumento,attr"`
		Serie            string       `xml:"Serie,attr"`
		Folio            string       `xml:"Folio,attr"`
		MonedaDR         string       `xml:"MonedaDR,attr"`
		EquivalenciaDR   string       `xml:"EquivalenciaDR,attr"`
		MetodoDePagoDR   string       `xml:"MetodoDePagoDR,attr"`
		NumParcialidad   string       `xml:"NumParcialidad,attr"`
		ImpSaldoAnt      string       `xml:"ImpSaldoAnt,attr"`
		ImpPagado        string       `xml:"ImpPagado,attr"`
		ImpSaldoInsoluto string       `xml:"ImpSaldoInsoluto,attr"`
		ObjetoImpDR      string       `xml:"ObjetoImpDR,attr"`
		ImpuestosDR      *ImpuestosDR `xml:"ImpuestosDR"`
	}
	type Pago struct {
		FechaPago        string             `xml:"FechaPago,attr"`
		FormaDePagoP     string             `xml:"FormaDePagoP,attr"`
		MonedaP          string             `xml:"MonedaP,attr"`
		Monto            string             `xml:"Monto,attr"`
		TipoCambioP      string             `xml:"TipoCambioP,attr"`
		NumOperacion     string             `xml:"NumOperacion,attr"`
		RfcEmisorCtaOrd  string             `xml:"RfcEmisorCtaOrd,attr"`
		NomBancoOrdExt   string             `xml:"NomBancoOrdExt,attr"`
		CtaOrdenante     string             `xml:"CtaOrdenante,attr"`
		RfcEmisorCtaBen  string             `xml:"RfcEmisorCtaBen,attr"`
		CtaBeneficiario  string             `xml:"CtaBeneficiario,attr"`
		TipoCadPago      string             `xml:"TipoCadPago,attr"`
		CertPago         string             `xml:"CertPago,attr"`
		CadPago          string             `xml:"CadPago,attr"`
		SelloPago        string             `xml:"SelloPago,attr"`
		DoctoRelacionado []DoctoRelacionado `xml:"DoctoRelacionado"`
	}
	type PagosComp struct {
		Pago []Pago `xml:"Pago"`
	}
	type Complemento struct {
		Pagos *PagosComp `xml:"Pagos"`
	}
	type Comprobante struct {
		XMLName     xml.Name    `xml:"Comprobante"`
		Complemento Complemento `xml:"Complemento"`
	}

	var comp Comprobante
	if err := xml.Unmarshal([]byte(xmlContent), &comp); err != nil {
		return nil, fmt.Errorf("unmarshal for Pagos: %w", err)
	}
	if comp.Complemento.Pagos == nil || len(comp.Complemento.Pagos.Pago) == 0 {
		return nil, nil
	}

	result := make([]pagoParsed, 0, len(comp.Complemento.Pagos.Pago))
	for _, p := range comp.Complemento.Pagos.Pago {
		pp := pagoParsed{
			FechaPago:       parseDatetime(strings.TrimSpace(p.FechaPago)),
			FormaDePagoP:    p.FormaDePagoP,
			MonedaP:         p.MonedaP,
			Monto:           parseFloatStr(p.Monto),
			TipoCambioP:     parseFloatStr(p.TipoCambioP),
			NumOperacion:    p.NumOperacion,
			RfcEmisorCtaOrd: p.RfcEmisorCtaOrd,
			NomBancoOrdExt:  p.NomBancoOrdExt,
			CtaOrdenante:    p.CtaOrdenante,
			RfcEmisorCtaBen: p.RfcEmisorCtaBen,
			CtaBeneficiario: p.CtaBeneficiario,
			TipoCadPago:     p.TipoCadPago,
			CertPago:        p.CertPago,
			CadPago:         p.CadPago,
			SelloPago:       p.SelloPago,
		}
		for _, d := range p.DoctoRelacionado {
			dp := doctoRelParsed{
				IdDocumento:      d.IdDocumento,
				Serie:            d.Serie,
				Folio:            d.Folio,
				MonedaDR:         d.MonedaDR,
				EquivalenciaDR:   parseFloatStr(d.EquivalenciaDR),
				MetodoDePagoDR:   d.MetodoDePagoDR,
				NumParcialidad:   int64(parseFloatStr(d.NumParcialidad)),
				ImpSaldoAnt:      parseFloatStr(d.ImpSaldoAnt),
				ImpPagado:        parseFloatStr(d.ImpPagado),
				ImpSaldoInsoluto: parseFloatStr(d.ImpSaldoInsoluto),
				ObjetoImpDR:      d.ObjetoImpDR,
			}
			if d.ImpuestosDR != nil {
				if d.ImpuestosDR.Traslados != nil {
					for _, t := range d.ImpuestosDR.Traslados.Traslado {
						dp.TrasladosDR = append(dp.TrasladosDR, trasladoDRParsed{
							BaseDR:       parseFloatStr(t.BaseDR),
							ImpuestoDR:   t.ImpuestoDR,
							TipoFactorDR: t.TipoFactorDR,
							TasaOCuotaDR: parseFloatStr(t.TasaOCuotaDR),
							ImporteDR:    parseFloatStr(t.ImporteDR),
						})
					}
				}
				if d.ImpuestosDR.Retenciones != nil {
					for _, r := range d.ImpuestosDR.Retenciones.Retencion {
						dp.RetencionesDR = append(dp.RetencionesDR, retencionDRParsed{
							ImpuestoDR: r.ImpuestoDR,
							ImporteDR:  parseFloatStr(r.ImporteDR),
						})
					}
				}
			}
			pp.DoctoRelacionados = append(pp.DoctoRelacionados, dp)
		}
		result = append(result, pp)
	}
	return result, nil
}
