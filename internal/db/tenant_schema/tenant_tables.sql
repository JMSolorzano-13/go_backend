-- Tenant DDL embedded by go_backend (internal/db/tenant_apply.go).
-- Token __TENANT_SCHEMA_QUOTED__ is replaced with the quoted company UUID schema name.
-- Regenerate when fastapi_backend/chalicelib/alembic_tenant head changes:
--   bash go_backend/scripts/regenerate_tenant_tables_sql.sh  (from monorepo root)
--
-- Originally from pg_dump 15.17; pg_dump session preamble (SET search_path, etc.)
-- stripped because it poisons the connection pool's search_path on pooled connections.




--
-- Name: attachment_state; Type: TYPE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TYPE __TENANT_SCHEMA_QUOTED__.attachment_state AS ENUM (
    'PENDING',
    'CONFIRMED',
    'DELETED'
);


ALTER TYPE __TENANT_SCHEMA_QUOTED__.attachment_state OWNER TO solcpuser;

-- pg_dump captures column types as public.* because Alembic creates them there.
-- On fresh Azure DBs (Go migrations only) the public types do not exist.
-- Define every enum inside the tenant schema so the DDL is self-contained.

CREATE TYPE __TENANT_SCHEMA_QUOTED__.add_sync_request_state_enum AS ENUM (
    'DRAFT', 'SENT', 'ERROR'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.cfdiexportstate AS ENUM (
    'SENT', 'TO_DOWNLOAD', 'ERROR'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.exportdatatype AS ENUM (
    'CFDI', 'IVA', 'ISR'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_nom_version AS ENUM (
    '1.1', '1.2'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_tipo_nomina AS ENUM (
    'O', 'E'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_tipo_contrato AS ENUM (
    '01','02','03','04','05','06','07','08','09','10','99'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_tipo_jornada AS ENUM (
    '01','02','03','04','05','06','07','08','99'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_tipo_regimen AS ENUM (
    '02','03','04','05','06','07','08','09','10','11','12','13','99'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_riesgo_puesto AS ENUM (
    '1','2','3','4','5','99'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_periodicidad_pago AS ENUM (
    '01','02','03','04','05','06','07','08','09','10','99'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_banco AS ENUM (
    '002','006','009','012','014','019','021','030','032','036','037','042','044',
    '058','059','060','062','072','102','103','106','108','110','112','113','116',
    '124','126','127','128','129','130','131','132','133','134','135','136','137',
    '138','139','140','141','143','145','147','148','149','150','151','152','153',
    '154','155','156','157','158','159','160','166','168',
    '600','601','602','605','606','607','608','610','614','615','616','617','618',
    '619','620','621','622','623','626','627','628','629','630','631','632','633',
    '634','636','637','638','640','642','646','647','648','649','651','652','653',
    '655','656','659','670','901','902'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.enum_clave_ent_fed AS ENUM (
    'AGU','BCN','BCS','CAM','CHP','CHH','COA','COL','CMX','DIF','DUR','GUA','GRO',
    'HID','JAL','MEX','MIC','MOR','NAY','NLE','OAX','PUE','QUE','ROO','SLP','SIN',
    'SON','TAB','TAM','TLA','VER','YUC','ZAC',
    'AL','AK','AZ','AR','CA','NC','SC','CO','CT','ND','SD','DE','FL','GA','HI','ID',
    'IL','IN','IA','KS','KY','LA','ME','MD','MA','MI','MN','MS','MO','MT','NE','NV',
    'NJ','NY','NH','NM','OH','OK','OR','PA','RI','TN','TX','UT','VT','VA','WV','WA',
    'WI','WY',
    'ON','QC','NS','NB','MB','BC','PE','SK','AB','NL','NT','YT','UN'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.downloadtype AS ENUM (
    'ISSUED', 'RECEIVED', 'BOTH'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.requesttype AS ENUM (
    'CFDI', 'METADATA', 'BOTH', 'CANCELLATION'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.querystate AS ENUM (
    'DRAFT','SENT','TO_DOWNLOAD','DOWNLOADED','TO_SCRAP','DELAYED','PROCESSING',
    'ERROR_IN_CERTS','ERROR_SAT_WS_UNKNOWN','ERROR_SAT_WS_INTERNAL','ERROR_TOO_BIG',
    'TIME_LIMIT_REACHED','ERROR','SCRAP_FAILED','CANT_SCRAP','MANUALLY_CANCELLED',
    'SPLITTED','INFORMATION_NOT_FOUND','SUBSTITUTED','SUBSTITUTED_TO_SCRAP',
    'PROCESSED','SCRAPPED'
);

CREATE TYPE __TENANT_SCHEMA_QUOTED__.satdownloadtechnology AS ENUM (
    'WebService', 'Scraper'
);

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: add_sync_request; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.add_sync_request (
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    start date NOT NULL,
    "end" date NOT NULL,
    xmls_to_send integer NOT NULL,
    xmls_to_send_pending integer NOT NULL,
    xmls_to_send_total double precision NOT NULL,
    cfdis_to_cancel integer NOT NULL,
    cfdis_to_cancel_pending integer NOT NULL,
    cfdis_to_cancel_total double precision NOT NULL,
    pasto_sent_identifier uuid,
    pasto_cancel_identifier uuid,
    state __TENANT_SCHEMA_QUOTED__.add_sync_request_state_enum NOT NULL,
    manually_triggered boolean DEFAULT false NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.add_sync_request OWNER TO solcpuser;

--
-- Name: alembic_version; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.alembic_version (
    version_num character varying(32) NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.alembic_version OWNER TO solcpuser;

--
-- Name: attachment; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.attachment (
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    cfdi_uuid uuid NOT NULL,
    creator_identifier uuid NOT NULL,
    deleter_identifier uuid,
    deleted_at timestamp without time zone,
    size integer NOT NULL,
    file_name character varying NOT NULL,
    content_hash character varying NOT NULL,
    s3_key character varying NOT NULL,
    state __TENANT_SCHEMA_QUOTED__.attachment_state NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.attachment OWNER TO solcpuser;

--
-- Name: cfdi; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.cfdi (
    is_issued boolean NOT NULL,
    "UUID" uuid NOT NULL,
    "Fecha" timestamp without time zone NOT NULL,
    "Total" numeric NOT NULL,
    "Folio" character varying,
    "Serie" character varying,
    "NoCertificado" character varying,
    "Certificado" character varying,
    "TipoDeComprobante" character varying NOT NULL,
    "LugarExpedicion" character varying,
    "FormaPago" character varying,
    "MetodoPago" character varying,
    "Moneda" character varying,
    "SubTotal" numeric,
    "RfcEmisor" character varying NOT NULL,
    "NombreEmisor" character varying,
    "RfcReceptor" character varying NOT NULL,
    "NombreReceptor" character varying,
    "RfcPac" character varying,
    "FechaCertificacionSat" timestamp without time zone NOT NULL,
    "Estatus" boolean NOT NULL,
    "ExcludeFromIVA" boolean DEFAULT false NOT NULL,
    "ExcludeFromISR" boolean DEFAULT false NOT NULL,
    "FechaCancelacion" timestamp without time zone,
    "TipoCambio" numeric,
    "Conceptos" character varying,
    "Version" character varying,
    "Sello" character varying,
    "UsoCFDIReceptor" character varying,
    "RegimenFiscalEmisor" character varying,
    "CondicionesDePago" character varying,
    "CfdiRelacionados" character varying,
    "Neto" numeric,
    "TrasladosIVA" numeric,
    "TrasladosIEPS" numeric,
    "TrasladosISR" numeric,
    "RetencionesIVA" numeric,
    "RetencionesIEPS" numeric,
    "RetencionesISR" numeric,
    "FechaFiltro" timestamp without time zone NOT NULL,
    "Impuestos" character varying,
    "Exportacion" character varying,
    "Periodicidad" character varying,
    "Meses" character varying,
    "Year" character varying,
    "DomicilioFiscalReceptor" character varying,
    "RegimenFiscalReceptor" character varying,
    "TotalMXN" numeric,
    "SubTotalMXN" numeric,
    "NetoMXN" numeric,
    "DescuentoMXN" numeric,
    "TrasladosIVAMXN" numeric,
    "TrasladosIEPSMXN" numeric,
    "TrasladosISRMXN" numeric,
    "RetencionesIVAMXN" numeric,
    "RetencionesIEPSMXN" numeric,
    "RetencionesISRMXN" numeric,
    "NoCertificadoSAT" character varying,
    "SelloSAT" character varying,
    "Descuento" numeric,
    "PaymentDate" timestamp without time zone NOT NULL,
    "TipoDeComprobante_I_MetodoPago_PPD" boolean NOT NULL,
    "TipoDeComprobante_I_MetodoPago_PUE" boolean NOT NULL,
    "TipoDeComprobante_E_MetodoPago_PPD" boolean NOT NULL,
    "TipoDeComprobante_E_CfdiRelacionados_None" boolean NOT NULL,
    cancelled_other_month boolean NOT NULL,
    other_rfc character varying,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    active boolean NOT NULL,
    is_too_big boolean DEFAULT false NOT NULL,
    from_xml boolean NOT NULL,
    xml_content xml,
    add_exists boolean DEFAULT false NOT NULL,
    add_cancel_date timestamp without time zone,
    "BaseIVA16" numeric,
    "BaseIVA8" numeric,
    "BaseIVA0" numeric,
    "BaseIVAExento" numeric,
    "IVATrasladado16" numeric,
    "IVATrasladado8" numeric,
    pr_count numeric DEFAULT '0'::numeric NOT NULL,
    company_identifier uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.cfdi OWNER TO solcpuser;

--
-- Name: cfdi_export; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.cfdi_export (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    url character varying,
    state __TENANT_SCHEMA_QUOTED__.cfdiexportstate,
    expiration_date timestamp without time zone,
    start character varying,
    "end" character varying,
    cfdi_type character varying,
    download_type character varying,
    format character varying,
    external_request boolean,
    export_data_type __TENANT_SCHEMA_QUOTED__.exportdatatype DEFAULT 'CFDI'::__TENANT_SCHEMA_QUOTED__.exportdatatype,
    displayed_name character varying DEFAULT ''::character varying NOT NULL,
    file_name character varying DEFAULT ''::character varying NOT NULL,
    domain character varying
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.cfdi_export OWNER TO solcpuser;

--
-- Name: cfdi_relation; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.cfdi_relation (
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now(),
    uuid_origin uuid NOT NULL,
    "TipoDeComprobante" character varying NOT NULL,
    is_issued boolean NOT NULL,
    "Estatus" boolean NOT NULL,
    uuid_related uuid NOT NULL,
    "TipoRelacion" character varying NOT NULL,
    company_identifier uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.cfdi_relation OWNER TO solcpuser;

--
-- Name: nomina; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.nomina (
    cfdi_uuid uuid NOT NULL,
    "Version" __TENANT_SCHEMA_QUOTED__.enum_nom_version NOT NULL,
    "TipoNomina" __TENANT_SCHEMA_QUOTED__.enum_tipo_nomina NOT NULL,
    "FechaPago" timestamp without time zone NOT NULL,
    "FechaInicialPago" timestamp without time zone NOT NULL,
    "FechaFinalPago" timestamp without time zone NOT NULL,
    "NumDiasPagados" numeric NOT NULL,
    "TotalPercepciones" numeric,
    "TotalDeducciones" numeric,
    "TotalOtrosPagos" numeric,
    "EmisorRegistroPatronal" character varying,
    "ReceptorCurp" character varying NOT NULL,
    "ReceptorNumSeguridadSocial" character varying,
    "ReceptorFechaInicioRelLaboral" timestamp without time zone,
    "ReceptorAntigüedad" character varying,
    "ReceptorTipoContrato" __TENANT_SCHEMA_QUOTED__.enum_tipo_contrato NOT NULL,
    "ReceptorSindicalizado" boolean,
    "ReceptorTipoJornada" __TENANT_SCHEMA_QUOTED__.enum_tipo_jornada,
    "ReceptorTipoRegimen" __TENANT_SCHEMA_QUOTED__.enum_tipo_regimen NOT NULL,
    "ReceptorNumEmpleado" character varying NOT NULL,
    "ReceptorDepartamento" character varying,
    "ReceptorPuesto" character varying,
    "ReceptorRiesgoPuesto" __TENANT_SCHEMA_QUOTED__.enum_riesgo_puesto,
    "ReceptorPeriodicidadPago" __TENANT_SCHEMA_QUOTED__.enum_periodicidad_pago NOT NULL,
    "ReceptorBanco" __TENANT_SCHEMA_QUOTED__.enum_banco,
    "ReceptorCuentaBancaria" character varying,
    "ReceptorSalarioBaseCotApor" numeric,
    "ReceptorSalarioDiarioIntegrado" numeric,
    "ReceptorClaveEntFed" __TENANT_SCHEMA_QUOTED__.enum_clave_ent_fed NOT NULL,
    "PercepcionesTotalSueldos" numeric,
    "PercepcionesTotalGravado" numeric,
    "PercepcionesTotalExento" numeric,
    "PercepcionesSeparacionIndemnizacion" numeric,
    "PercepcionesJubilacionPensionRetiro" numeric,
    "DeduccionesTotalOtrasDeducciones" numeric,
    "DeduccionesTotalImpuestosRetenidos" numeric,
    "SubsidioCausado" numeric,
    "AjusteISRRetenido" numeric,
    company_identifier uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.nomina OWNER TO solcpuser;

--
-- Name: payment; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.payment (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    is_issued boolean DEFAULT false NOT NULL,
    uuid_origin uuid NOT NULL,
    index integer NOT NULL,
    "FechaPago" timestamp without time zone NOT NULL,
    "FormaDePagoP" character varying NOT NULL,
    "MonedaP" character varying NOT NULL,
    "Monto" numeric NOT NULL,
    "TipoCambioP" numeric,
    "NumOperacion" character varying,
    "RfcEmisorCtaOrd" character varying,
    "NomBancoOrdExt" character varying,
    "CtaOrdenante" character varying,
    "RfcEmisorCtaBen" character varying,
    "CtaBeneficiario" character varying,
    "TipoCadPago" character varying,
    "CertPago" character varying,
    "CadPago" character varying,
    "SelloPago" character varying,
    "Estatus" boolean DEFAULT true NOT NULL,
    company_identifier uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.payment OWNER TO solcpuser;

--
-- Name: payment_relation; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.payment_relation (
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    is_issued boolean DEFAULT false NOT NULL,
    created_at timestamp without time zone DEFAULT now(),
    payment_identifier uuid NOT NULL,
    "UUID" uuid NOT NULL,
    "UUID_related" uuid NOT NULL,
    "FechaPago" timestamp without time zone NOT NULL,
    "Serie" character varying,
    "Folio" character varying,
    "MonedaDR" character varying NOT NULL,
    "EquivalenciaDR" numeric,
    "MetodoDePagoDR" character varying,
    "NumParcialidad" integer NOT NULL,
    "ImpSaldoAnt" numeric NOT NULL,
    "ImpPagado" numeric NOT NULL,
    "ImpPagadoMXN" numeric NOT NULL,
    "ImpSaldoInsoluto" numeric NOT NULL,
    active boolean NOT NULL,
    applied boolean NOT NULL,
    "ObjetoImpDR" character varying,
    "BaseIVA16" numeric NOT NULL,
    "BaseIVA8" numeric NOT NULL,
    "BaseIVA0" numeric NOT NULL,
    "BaseIVAExento" numeric NOT NULL,
    "IVATrasladado16" numeric NOT NULL,
    "IVATrasladado8" numeric NOT NULL,
    "TrasladosIVAMXN" numeric NOT NULL,
    "RetencionesIVAMXN" numeric,
    "RetencionesDR" jsonb,
    "TrasladosDR" jsonb,
    "Estatus" boolean DEFAULT true NOT NULL,
    "ExcludeFromIVA" boolean DEFAULT false NOT NULL,
    company_identifier uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    "ExcludeFromISR" boolean DEFAULT false NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.payment_relation OWNER TO solcpuser;

--
-- Name: poliza; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.poliza (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    fecha timestamp without time zone NOT NULL,
    tipo character varying NOT NULL,
    numero character varying NOT NULL,
    concepto character varying,
    sistema_origen character varying
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.poliza OWNER TO solcpuser;

--
-- Name: poliza_cfdi; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.poliza_cfdi (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    poliza_identifier uuid NOT NULL,
    uuid_related uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.poliza_cfdi OWNER TO solcpuser;

--
-- Name: poliza_movimiento; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.poliza_movimiento (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    numerador character varying,
    cuenta_contable character varying,
    nombre character varying,
    cargo numeric NOT NULL,
    abono numeric NOT NULL,
    cargo_me numeric NOT NULL,
    abono_me numeric NOT NULL,
    concepto character varying,
    referencia character varying,
    poliza_identifier uuid NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.poliza_movimiento OWNER TO solcpuser;

--
-- Name: sat_query; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.sat_query (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    identifier uuid NOT NULL,
    name character varying NOT NULL,
    start timestamp without time zone NOT NULL,
    "end" timestamp without time zone NOT NULL,
    download_type __TENANT_SCHEMA_QUOTED__.downloadtype NOT NULL,
    request_type __TENANT_SCHEMA_QUOTED__.requesttype NOT NULL,
    packages json,
    cfdis_qty integer,
    state __TENANT_SCHEMA_QUOTED__.querystate NOT NULL,
    sent_date timestamp without time zone,
    is_manual boolean DEFAULT false,
    technology __TENANT_SCHEMA_QUOTED__.satdownloadtechnology DEFAULT 'WebService'::__TENANT_SCHEMA_QUOTED__.satdownloadtechnology NOT NULL,
    origin_identifier uuid
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.sat_query OWNER TO solcpuser;

--
-- Name: user_config; Type: TABLE; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE TABLE __TENANT_SCHEMA_QUOTED__.user_config (
    created_at timestamp without time zone,
    updated_at timestamp without time zone,
    user_identifier uuid NOT NULL,
    data json NOT NULL
);


ALTER TABLE __TENANT_SCHEMA_QUOTED__.user_config OWNER TO solcpuser;

--
-- Name: add_sync_request add_sync_request_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.add_sync_request
    ADD CONSTRAINT add_sync_request_pkey PRIMARY KEY (identifier);


--
-- Name: alembic_version alembic_version_pkc; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.alembic_version
    ADD CONSTRAINT alembic_version_pkc PRIMARY KEY (version_num);


--
-- Name: attachment attachment_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.attachment
    ADD CONSTRAINT attachment_pkey PRIMARY KEY (identifier);


--
-- Name: cfdi_export cfdi_export_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.cfdi_export
    ADD CONSTRAINT cfdi_export_pkey PRIMARY KEY (identifier);


--
-- Name: cfdi cfdi_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.cfdi
    ADD CONSTRAINT cfdi_pkey PRIMARY KEY (company_identifier, is_issued, "UUID");


--
-- Name: cfdi_relation cfdi_relation_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.cfdi_relation
    ADD CONSTRAINT cfdi_relation_pkey PRIMARY KEY (company_identifier, is_issued, identifier);


--
-- Name: nomina nomina_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.nomina
    ADD CONSTRAINT nomina_pkey PRIMARY KEY (company_identifier, cfdi_uuid);


--
-- Name: payment payment_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.payment
    ADD CONSTRAINT payment_pkey PRIMARY KEY (company_identifier, identifier);


--
-- Name: payment_relation payment_relation_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.payment_relation
    ADD CONSTRAINT payment_relation_pkey PRIMARY KEY (company_identifier, identifier);


--
-- Name: poliza_cfdi poliza_cfdi_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.poliza_cfdi
    ADD CONSTRAINT poliza_cfdi_pkey PRIMARY KEY (poliza_identifier, uuid_related);


--
-- Name: poliza_movimiento poliza_movimiento_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.poliza_movimiento
    ADD CONSTRAINT poliza_movimiento_pkey PRIMARY KEY (identifier);


--
-- Name: poliza poliza_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.poliza
    ADD CONSTRAINT poliza_pkey PRIMARY KEY (identifier);


--
-- Name: sat_query sat_query_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.sat_query
    ADD CONSTRAINT sat_query_pkey PRIMARY KEY (identifier);


--
-- Name: user_config user_config_pkey; Type: CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.user_config
    ADD CONSTRAINT user_config_pkey PRIMARY KEY (user_identifier);


--
-- Name: cfdi_Fecha_Estatus_from_xml_is_too_big_idx; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "cfdi_Fecha_Estatus_from_xml_is_too_big_idx" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("Fecha", "Estatus", from_xml, is_too_big) WHERE ("Estatus" AND (NOT from_xml) AND (NOT is_too_big));


--
-- Name: cfdi_add_exists_UUID_add_cancel_date_idx; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "cfdi_add_exists_UUID_add_cancel_date_idx" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree (add_exists, "UUID", add_cancel_date);


--
-- Name: cfdi_add_exists_UUID_idx; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "cfdi_add_exists_UUID_idx" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree (add_exists, "UUID");


--
-- Name: idx_attachment_cfdi_filename_unique; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE UNIQUE INDEX idx_attachment_cfdi_filename_unique ON __TENANT_SCHEMA_QUOTED__.attachment USING btree (cfdi_uuid, file_name) WHERE (state <> 'DELETED'::__TENANT_SCHEMA_QUOTED__.attachment_state);


--
-- Name: ix_add_sync_request_created_at; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_add_sync_request_created_at ON __TENANT_SCHEMA_QUOTED__.add_sync_request USING btree (created_at);


--
-- Name: ix_add_sync_request_state; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_add_sync_request_state ON __TENANT_SCHEMA_QUOTED__.add_sync_request USING btree (state);


--
-- Name: ix_attachment_cfdi_uuid; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_attachment_cfdi_uuid ON __TENANT_SCHEMA_QUOTED__.attachment USING btree (cfdi_uuid);


--
-- Name: ix_attachment_file_name; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_attachment_file_name ON __TENANT_SCHEMA_QUOTED__.attachment USING btree (file_name);


--
-- Name: ix_attachment_state; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_attachment_state ON __TENANT_SCHEMA_QUOTED__.attachment USING btree (state);


--
-- Name: ix_cfdi_FechaCancelacion; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_FechaCancelacion" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("FechaCancelacion");


--
-- Name: ix_cfdi_FechaFiltro; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_FechaFiltro" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("FechaFiltro");


--
-- Name: ix_cfdi_MetodoPago; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_MetodoPago" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("MetodoPago");


--
-- Name: ix_cfdi_PaymentDate; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_PaymentDate" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("PaymentDate");


--
-- Name: ix_cfdi_TipoDeComprobante; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_TipoDeComprobante" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("TipoDeComprobante");


--
-- Name: ix_cfdi_UUID; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_UUID" ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree ("UUID");


--
-- Name: ix_cfdi_created_at; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_created_at ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree (created_at);


--
-- Name: ix_cfdi_export_cfdi_type; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_cfdi_type ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (cfdi_type);


--
-- Name: ix_cfdi_export_displayed_name; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_displayed_name ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (displayed_name);


--
-- Name: ix_cfdi_export_download_type; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_download_type ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (download_type);


--
-- Name: ix_cfdi_export_end; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_end ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree ("end");


--
-- Name: ix_cfdi_export_expiration_date; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_expiration_date ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (expiration_date);


--
-- Name: ix_cfdi_export_export_data_type; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_export_data_type ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (export_data_type);


--
-- Name: ix_cfdi_export_file_name; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_file_name ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (file_name);


--
-- Name: ix_cfdi_export_format; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_format ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (format);


--
-- Name: ix_cfdi_export_start; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_start ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (start);


--
-- Name: ix_cfdi_export_state; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_state ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (state);


--
-- Name: ix_cfdi_export_url; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_export_url ON __TENANT_SCHEMA_QUOTED__.cfdi_export USING btree (url);


--
-- Name: ix_cfdi_other_rfc; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_other_rfc ON __TENANT_SCHEMA_QUOTED__.cfdi USING btree (other_rfc);


--
-- Name: ix_cfdi_relation_TipoDeComprobante; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_cfdi_relation_TipoDeComprobante" ON __TENANT_SCHEMA_QUOTED__.cfdi_relation USING btree ("TipoDeComprobante");


--
-- Name: ix_cfdi_relation_created_at; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_relation_created_at ON __TENANT_SCHEMA_QUOTED__.cfdi_relation USING btree (created_at);


--
-- Name: ix_cfdi_relation_uuid_origin; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_relation_uuid_origin ON __TENANT_SCHEMA_QUOTED__.cfdi_relation USING btree (uuid_origin);


--
-- Name: ix_cfdi_relation_uuid_related; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_cfdi_relation_uuid_related ON __TENANT_SCHEMA_QUOTED__.cfdi_relation USING btree (uuid_related);


--
-- Name: ix_nomina_EmisorRegistroPatronal; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_EmisorRegistroPatronal" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("EmisorRegistroPatronal");


--
-- Name: ix_nomina_FechaFinalPago; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_FechaFinalPago" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("FechaFinalPago");


--
-- Name: ix_nomina_FechaInicialPago; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_FechaInicialPago" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("FechaInicialPago");


--
-- Name: ix_nomina_FechaPago; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_FechaPago" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("FechaPago");


--
-- Name: ix_nomina_ReceptorBanco; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorBanco" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorBanco");


--
-- Name: ix_nomina_ReceptorClaveEntFed; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorClaveEntFed" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorClaveEntFed");


--
-- Name: ix_nomina_ReceptorCurp; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorCurp" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorCurp");


--
-- Name: ix_nomina_ReceptorDepartamento; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorDepartamento" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorDepartamento");


--
-- Name: ix_nomina_ReceptorNumEmpleado; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorNumEmpleado" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorNumEmpleado");


--
-- Name: ix_nomina_ReceptorNumSeguridadSocial; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorNumSeguridadSocial" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorNumSeguridadSocial");


--
-- Name: ix_nomina_ReceptorPeriodicidadPago; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorPeriodicidadPago" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorPeriodicidadPago");


--
-- Name: ix_nomina_ReceptorPuesto; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorPuesto" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorPuesto");


--
-- Name: ix_nomina_ReceptorRiesgoPuesto; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorRiesgoPuesto" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorRiesgoPuesto");


--
-- Name: ix_nomina_ReceptorTipoContrato; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorTipoContrato" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorTipoContrato");


--
-- Name: ix_nomina_ReceptorTipoJornada; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorTipoJornada" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorTipoJornada");


--
-- Name: ix_nomina_ReceptorTipoRegimen; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_ReceptorTipoRegimen" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("ReceptorTipoRegimen");


--
-- Name: ix_nomina_TipoNomina; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_TipoNomina" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("TipoNomina");


--
-- Name: ix_nomina_Version; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_nomina_Version" ON __TENANT_SCHEMA_QUOTED__.nomina USING btree ("Version");


--
-- Name: ix_nomina_cfdi_uuid; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_nomina_cfdi_uuid ON __TENANT_SCHEMA_QUOTED__.nomina USING btree (cfdi_uuid);


--
-- Name: ix_payment_index; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_payment_index ON __TENANT_SCHEMA_QUOTED__.payment USING btree (index);


--
-- Name: ix_payment_relation_UUID; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_payment_relation_UUID" ON __TENANT_SCHEMA_QUOTED__.payment_relation USING btree ("UUID");


--
-- Name: ix_payment_relation_UUID_related; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX "ix_payment_relation_UUID_related" ON __TENANT_SCHEMA_QUOTED__.payment_relation USING btree ("UUID_related");


--
-- Name: ix_payment_relation_created_at; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_payment_relation_created_at ON __TENANT_SCHEMA_QUOTED__.payment_relation USING btree (created_at);


--
-- Name: ix_payment_uuid_origin; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_payment_uuid_origin ON __TENANT_SCHEMA_QUOTED__.payment USING btree (uuid_origin);


--
-- Name: ix_poliza_movimiento_poliza_identifier; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_poliza_movimiento_poliza_identifier ON __TENANT_SCHEMA_QUOTED__.poliza_movimiento USING btree (poliza_identifier);


--
-- Name: ix_poliza_unique; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE UNIQUE INDEX ix_poliza_unique ON __TENANT_SCHEMA_QUOTED__.poliza USING btree (fecha, tipo, numero);


--
-- Name: ix_sat_query_download_type; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_download_type ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (download_type);


--
-- Name: ix_sat_query_end; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_end ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree ("end");


--
-- Name: ix_sat_query_name; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_name ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (name);


--
-- Name: ix_sat_query_request_type; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_request_type ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (request_type);


--
-- Name: ix_sat_query_sent_date; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_sent_date ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (sent_date);


--
-- Name: ix_sat_query_start; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_start ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (start);


--
-- Name: ix_sat_query_state; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_state ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (state);


--
-- Name: ix_sat_query_technology; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX ix_sat_query_technology ON __TENANT_SCHEMA_QUOTED__.sat_query USING btree (technology);


--
-- Name: payment_relation_company_identifier_is_issued_fecha_pago_index; Type: INDEX; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

CREATE INDEX payment_relation_company_identifier_is_issued_fecha_pago_index ON __TENANT_SCHEMA_QUOTED__.payment_relation USING btree (is_issued, "FechaPago");


--
-- Name: poliza_cfdi fk_poliza_cfdi_poliza_identifier_poliza; Type: FK CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.poliza_cfdi
    ADD CONSTRAINT fk_poliza_cfdi_poliza_identifier_poliza FOREIGN KEY (poliza_identifier) REFERENCES __TENANT_SCHEMA_QUOTED__.poliza(identifier) ON DELETE CASCADE;


--
-- Name: poliza_movimiento fk_poliza_movimiento_poliza_identifier_poliza; Type: FK CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.poliza_movimiento
    ADD CONSTRAINT fk_poliza_movimiento_poliza_identifier_poliza FOREIGN KEY (poliza_identifier) REFERENCES __TENANT_SCHEMA_QUOTED__.poliza(identifier) ON DELETE CASCADE;


--
-- Name: sat_query fk_sat_query_origin_identifier_sat_query; Type: FK CONSTRAINT; Schema: tenant-placeholder-uuid; Owner: solcpuser
--

ALTER TABLE ONLY __TENANT_SCHEMA_QUOTED__.sat_query
    ADD CONSTRAINT fk_sat_query_origin_identifier_sat_query FOREIGN KEY (origin_identifier) REFERENCES __TENANT_SCHEMA_QUOTED__.sat_query(identifier) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


