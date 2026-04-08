package sat

// DownloadType selects issued vs received CFDIs in SAT web service queries.
type DownloadType string

const (
	DownloadTypeIssued   DownloadType = "RfcEmisor"
	DownloadTypeReceived DownloadType = "RfcReceptor"
	DownloadTypeFolio    DownloadType = "Folio"
)

// RequestType selects whether to download full XMLs or lightweight metadata.
type RequestType string

const (
	RequestTypeCFDI     RequestType = "CFDI"
	RequestTypeMetadata RequestType = "Metadata"
)

// TipoComprobante is the SAT invoice type code.
type TipoComprobante string

const (
	TipoComprobanteIngreso  TipoComprobante = "I"
	TipoComprobanteEgreso   TipoComprobante = "E"
	TipoComprobanteTraslado TipoComprobante = "T"
	TipoComprobanteNomina   TipoComprobante = "N"
	TipoComprobantePago     TipoComprobante = "P"
)

// EstadoComprobante filters by invoice lifecycle status.
type EstadoComprobante string

const (
	EstadoComprobanteTodos     EstadoComprobante = "Todos"
	EstadoComprobanteCancelado EstadoComprobante = "Cancelado"
	EstadoComprobanteVigente   EstadoComprobante = "Vigente"
)

// VerifyQueryStatus is the EstadoSolicitud value returned by Verifica.
type VerifyQueryStatus int

const (
	VerifyStatusUnknown    VerifyQueryStatus = 0
	VerifyStatusAccepted   VerifyQueryStatus = 1
	VerifyStatusInProcess  VerifyQueryStatus = 2
	VerifyStatusFinished   VerifyQueryStatus = 3
	VerifyStatusError      VerifyQueryStatus = 4
	VerifyStatusRejected   VerifyQueryStatus = 5
	VerifyStatusExpired    VerifyQueryStatus = 6
)
