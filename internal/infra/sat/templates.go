package sat

// XML templates for SAT SOAP web service requests.
// These are exact ports of the Python templates in
// backend/chalicelib/mx_edi/connectors/sat/templates/*.xml
//
// Placeholders use {name} syntax and are replaced at runtime via templateReplace.

// --- Common templates ---

const tplEnvelope = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" xmlns:des="http://DescargaMasivaTerceros.sat.gob.mx" xmlns:xd="http://www.w3.org/2000/09/xmldsig#">
    <s:Header/>
    <s:Body>{content}</s:Body>
</s:Envelope>`

const tplSignature = `<Signature xmlns="http://www.w3.org/2000/09/xmldsig#">{signed_info}
    <SignatureValue>{signature_value}</SignatureValue>{key_info}
</Signature>`

const tplSignedInfo = `<SignedInfo xmlns="http://www.w3.org/2000/09/xmldsig#">
            <CanonicalizationMethod Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"></CanonicalizationMethod>
            <SignatureMethod Algorithm="http://www.w3.org/2000/09/xmldsig#rsa-sha1"></SignatureMethod>
            <Reference URI="{uri}">
                <Transforms>
                    <Transform Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"></Transform>
                </Transforms>
                <DigestMethod Algorithm="http://www.w3.org/2000/09/xmldsig#sha1"></DigestMethod>
                <DigestValue>{digest_value}</DigestValue>
            </Reference>
        </SignedInfo>`

const tplKeyInfo = `<KeyInfo>
    <X509Data>
        <X509IssuerSerial>
            <X509IssuerName>{issuer_name}</X509IssuerName>
            <X509SerialNumber>{serial_number}</X509SerialNumber>
        </X509IssuerSerial>
        <X509Certificate>{certificate}</X509Certificate>
    </X509Data>
</KeyInfo>`

// --- Login templates ---

const tplLoginEnvelope = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" xmlns:u="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">
  <s:Header>
    <o:Security s:mustUnderstand="1" xmlns:o="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">
      {timestamp_node}
      <o:BinarySecurityToken u:Id="_1" ValueType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-x509-token-profile-1.0#X509v3" EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">{binary_security_token}</o:BinarySecurityToken>
      <Signature xmlns="http://www.w3.org/2000/09/xmldsig#">
        {signed_info_node}
        <SignatureValue>{signature_value}</SignatureValue>
        <KeyInfo>
          <o:SecurityTokenReference>
            <o:Reference URI="#_1" ValueType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-x509-token-profile-1.0#X509v3"/>
          </o:SecurityTokenReference>
        </KeyInfo>
      </Signature>
    </o:Security>
  </s:Header>
  <s:Body>
    <Autentica xmlns="http://DescargaMasivaTerceros.gob.mx"/>
  </s:Body>
</s:Envelope>`

const tplTimestamp = `<u:Timestamp xmlns:u="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd" u:Id="_0">
        <u:Created>{created}</u:Created>
        <u:Expires>{expires}</u:Expires>
      </u:Timestamp>`

// --- Query templates (Solicitud) ---

// tplSolicitaDescarga is the legacy v1.4 format.
const tplSolicitaDescarga = `<des:SolicitaDescarga>
  <des:solicitud FechaFinal="{end}" FechaInicial="{start}" {rfc_issued} RfcSolicitante="{rfc}" TipoSolicitud="{request_type}">{rfc_received}{signature}</des:solicitud>
</des:SolicitaDescarga>`

// tplSolicitaDescargaEmitidos is the v1.5 format for issued invoices.
const tplSolicitaDescargaEmitidos = `<des:SolicitaDescargaEmitidos>
  <des:solicitud{complemento} EstadoComprobante="{estado_comprobante}" FechaFinal="{end}" FechaInicial="{start}"{rfc_a_cuenta_terceros}{rfc_receptor} RfcEmisor="{rfc_solicitante}"{tipo_comprobante} TipoSolicitud="{request_type}">{signature}</des:solicitud>
</des:SolicitaDescargaEmitidos>`

// tplSolicitaDescargaRecibidos is the v1.5 format for received invoices.
const tplSolicitaDescargaRecibidos = `<des:SolicitaDescargaRecibidos>
  <des:solicitud{complemento} EstadoComprobante="{estado_comprobante}" FechaFinal="{end}" FechaInicial="{start}"{rfc_a_cuenta_terceros} RfcReceptor="{rfc_solicitante}"{tipo_comprobante} TipoSolicitud="{request_type}">{signature}</des:solicitud>
</des:SolicitaDescargaRecibidos>`

// tplSolicitaDescargaFolio queries by specific folio UUID.
const tplSolicitaDescargaFolio = `<des:SolicitaDescargaFolio>
  <des:solicitud Folio="{folio}" {rfc_solicitante}>{signature}</des:solicitud>
</des:SolicitaDescargaFolio>`

// --- Verify template ---

const tplVerificaSolicitudDescarga = `<des:VerificaSolicitudDescarga xmlns:des="http://DescargaMasivaTerceros.sat.gob.mx">
    <des:solicitud IdSolicitud="{identifier}" RfcSolicitante="{rfc}">{signature}</des:solicitud>
</des:VerificaSolicitudDescarga>`

// --- Download template ---

const tplPeticionDescarga = `<des:PeticionDescargaMasivaTercerosEntrada>
    <des:peticionDescarga IdPaquete="{package_id}" RfcSolicitante="{rfc}">{signature}</des:peticionDescarga>
</des:PeticionDescargaMasivaTercerosEntrada>`

// --- SOAP endpoints ---

const (
	urlAutenticacion      = "https://cfdidescargamasivasolicitud.clouda.sat.gob.mx/Autenticacion/Autenticacion.svc"
	urlSolicitaDescarga   = "https://cfdidescargamasivasolicitud.clouda.sat.gob.mx/SolicitaDescargaService.svc"
	urlVerificaSolicitud  = "https://cfdidescargamasivasolicitud.clouda.sat.gob.mx/VerificaSolicitudDescargaService.svc"
	urlDescargaMasiva     = "https://cfdidescargamasiva.clouda.sat.gob.mx/DescargaMasivaService.svc"

	soapActionAutentica  = "http://DescargaMasivaTerceros.gob.mx/IAutenticacion/Autentica"
	soapActionSolicita   = "http://DescargaMasivaTerceros.sat.gob.mx/ISolicitaDescargaService/"
	soapActionVerifica   = "http://DescargaMasivaTerceros.sat.gob.mx/IVerificaSolicitudDescargaService/VerificaSolicitudDescarga"
	soapActionDescarga   = "http://DescargaMasivaTerceros.sat.gob.mx/IDescargaMasivaTercerosService/Descargar"
)
