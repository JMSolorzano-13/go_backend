-- Control (public) schema: enums, tables, indexes.
-- Derived from Python Alembic head (108920959f2a) + password_hash for self-auth.

-- Enums
DO $$ BEGIN
  CREATE TYPE stateenum AS ENUM ('DEFINITIVE','DISTORTED','ALLEGED','FAVORABLE_JUDGMENT');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
  CREATE TYPE enum_permission_role AS ENUM ('OPERATOR','PAYROLL');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$ BEGIN
  CREATE TYPE enum_notification_config_notification_type AS ENUM ('ERROR','EFOS','CANCELED');
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- user
CREATE TABLE IF NOT EXISTS public."user" (
  id                              SERIAL PRIMARY KEY,
  identifier                      UUID UNIQUE,
  name                            VARCHAR,
  email                           VARCHAR NOT NULL,
  password_hash                   VARCHAR,
  cognito_sub                     VARCHAR UNIQUE,
  invited_by_id                   INTEGER REFERENCES public."user"(id) ON DELETE SET NULL,
  source_name                     VARCHAR,
  phone                           VARCHAR,
  odoo_identifier                 INTEGER,
  stripe_identifier               VARCHAR,
  stripe_subscription_identifier  VARCHAR,
  created_at                      TIMESTAMP DEFAULT NOW(),
  updated_at                      TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_user_email ON public."user"(email);
CREATE INDEX IF NOT EXISTS ix_user_name ON public."user"(name);
CREATE INDEX IF NOT EXISTS ix_user_invited_by_id ON public."user"(invited_by_id);
CREATE INDEX IF NOT EXISTS ix_user_source_name ON public."user"(source_name);

-- workspace
CREATE TABLE IF NOT EXISTS public.workspace (
  id                  SERIAL PRIMARY KEY,
  identifier          UUID UNIQUE,
  name                VARCHAR,
  owner_id            INTEGER REFERENCES public."user"(id) ON DELETE RESTRICT,
  license             JSONB,
  valid_until         TIMESTAMP,
  odoo_id             INTEGER,
  stripe_status       VARCHAR,
  pasto_worker_id     VARCHAR UNIQUE,
  pasto_license_key   VARCHAR UNIQUE,
  pasto_installed     BOOLEAN,
  pasto_worker_token  VARCHAR,
  add_permission      BOOLEAN,
  created_at          TIMESTAMP DEFAULT NOW(),
  updated_at          TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_workspace_owner_id ON public.workspace(owner_id);
CREATE INDEX IF NOT EXISTS ix_workspace_name ON public.workspace(name);
CREATE INDEX IF NOT EXISTS ix_workspace_stripe_status ON public.workspace(stripe_status);
CREATE INDEX IF NOT EXISTS ix_workspace_valid_until ON public.workspace(valid_until);
CREATE INDEX IF NOT EXISTS ix_workspace_add_permission ON public.workspace(add_permission);

-- pasto_company
CREATE TABLE IF NOT EXISTS public.pasto_company (
  pasto_company_id     UUID PRIMARY KEY,
  workspace_identifier UUID NOT NULL REFERENCES public.workspace(identifier) ON DELETE CASCADE,
  name                 VARCHAR NOT NULL,
  alias                VARCHAR NOT NULL,
  rfc                  VARCHAR NOT NULL,
  bdd                  VARCHAR DEFAULT 'Base de datos no identificada',
  system               VARCHAR DEFAULT 'Sistema no identificado',
  created_at           TIMESTAMP DEFAULT NOW(),
  updated_at           TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_pasto_company_workspace_identifier ON public.pasto_company(workspace_identifier);

-- company
CREATE TABLE IF NOT EXISTS public.company (
  id                        SERIAL PRIMARY KEY,
  identifier                UUID UNIQUE,
  name                      VARCHAR NOT NULL,
  workspace_id              INTEGER REFERENCES public.workspace(id) ON DELETE CASCADE,
  workspace_identifier      UUID REFERENCES public.workspace(identifier) ON DELETE CASCADE,
  rfc                       VARCHAR,
  active                    BOOLEAN DEFAULT TRUE,
  have_certificates         BOOLEAN DEFAULT FALSE,
  has_valid_certs           BOOLEAN DEFAULT FALSE,
  emails_to_send_efos       JSONB,
  emails_to_send_errors     JSONB,
  emails_to_send_canceled   JSONB,
  historic_downloaded       BOOLEAN DEFAULT FALSE,
  last_ws_download          TIMESTAMP,
  exceed_metadata_limit     BOOLEAN NOT NULL DEFAULT FALSE,
  permission_to_sync        BOOLEAN NOT NULL DEFAULT FALSE,
  last_notification         TIMESTAMP,
  pasto_company_identifier  UUID REFERENCES public.pasto_company(pasto_company_id) ON DELETE SET NULL,
  pasto_last_metadata_sync  TIMESTAMP,
  add_auto_sync             BOOLEAN DEFAULT FALSE,
  tenant_db_host            VARCHAR,
  tenant_db_port            INTEGER,
  tenant_db_name            VARCHAR,
  tenant_db_user            VARCHAR,
  tenant_db_password        VARCHAR,
  tenant_db_schema          VARCHAR,
  data                      JSONB NOT NULL DEFAULT '{"scrap_status_constancy":{"current_status":"","updated_at":""},"scrap_status_order":{"current_status":"","updated_at":""}}'::jsonb,
  created_at                TIMESTAMP DEFAULT NOW(),
  updated_at                TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_company_name ON public.company(name);
CREATE INDEX IF NOT EXISTS ix_company_rfc ON public.company(rfc);
CREATE INDEX IF NOT EXISTS ix_company_workspace_id ON public.company(workspace_id);
CREATE INDEX IF NOT EXISTS ix_company_workspace_identifier ON public.company(workspace_identifier);
CREATE INDEX IF NOT EXISTS ix_company_pasto_company_identifier ON public.company(pasto_company_identifier);
CREATE INDEX IF NOT EXISTS ix_company_has_valid_certs ON public.company(has_valid_certs);
CREATE INDEX IF NOT EXISTS ix_company_have_certificates ON public.company(have_certificates);
CREATE INDEX IF NOT EXISTS ix_company_add_auto_sync ON public.company(add_auto_sync);

-- permission
CREATE TABLE IF NOT EXISTS public.permission (
  id           SERIAL PRIMARY KEY,
  identifier   UUID UNIQUE,
  user_id      INTEGER NOT NULL REFERENCES public."user"(id) ON DELETE CASCADE,
  company_id   INTEGER NOT NULL REFERENCES public.company(id) ON DELETE CASCADE,
  role         enum_permission_role NOT NULL,
  created_at   TIMESTAMP DEFAULT NOW(),
  updated_at   TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_permission_user_id ON public.permission(user_id);
CREATE INDEX IF NOT EXISTS ix_permission_company_id ON public.permission(company_id);
CREATE INDEX IF NOT EXISTS ix_permission_role ON public.permission(role);

-- notification_config
CREATE TABLE IF NOT EXISTS public.notification_config (
  id                   SERIAL PRIMARY KEY,
  identifier           UUID UNIQUE,
  user_id              INTEGER NOT NULL REFERENCES public."user"(id) ON DELETE CASCADE,
  workspace_id         INTEGER NOT NULL,
  workspace_identifier UUID REFERENCES public.workspace(identifier) ON DELETE CASCADE,
  notification_type    enum_notification_config_notification_type NOT NULL,
  created_at           TIMESTAMP DEFAULT NOW(),
  updated_at           TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_notification_config_user_id ON public.notification_config(user_id);
CREATE INDEX IF NOT EXISTS ix_notification_config_workspace_id ON public.notification_config(workspace_id);
CREATE INDEX IF NOT EXISTS ix_notification_config_workspace_identifier ON public.notification_config(workspace_identifier);

-- param
CREATE TABLE IF NOT EXISTS public.param (
  id         SERIAL PRIMARY KEY,
  identifier UUID UNIQUE,
  name       VARCHAR NOT NULL,
  value      VARCHAR,
  created_at TIMESTAMP DEFAULT NOW(),
  updated_at TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_param_name ON public.param(name);

-- product
CREATE TABLE IF NOT EXISTS public.product (
  stripe_identifier       VARCHAR PRIMARY KEY,
  characteristics         JSON NOT NULL,
  price                   INTEGER NOT NULL,
  stripe_price_identifier VARCHAR NOT NULL,
  stripe_name             VARCHAR NOT NULL,
  created_at              TIMESTAMP DEFAULT NOW(),
  updated_at              TIMESTAMP
);

-- efos
CREATE TABLE IF NOT EXISTS public.efos (
  id                                     SERIAL PRIMARY KEY,
  identifier                             UUID UNIQUE,
  no                                     INTEGER NOT NULL,
  rfc                                    VARCHAR NOT NULL,
  name                                   VARCHAR NOT NULL,
  state                                  stateenum NOT NULL,
  sat_office_alleged                     VARCHAR,
  sat_publish_alleged_date               VARCHAR,
  dof_office_alleged                     VARCHAR,
  dof_publish_alleged_date               VARCHAR,
  sat_office_distored                    VARCHAR,
  sat_publish_distored_date              VARCHAR,
  dof_office_distored                    VARCHAR,
  dof_publish_distored_date              VARCHAR,
  sat_office_definitive                  VARCHAR,
  sat_publish_definitive_date            VARCHAR,
  dof_office_definitive                  VARCHAR,
  dof_publish_definitive_date            VARCHAR,
  sat_office_favorable_judgement         VARCHAR,
  sat_publish_favorable_judgement_date   VARCHAR,
  dof_office_favorable_judgement         VARCHAR,
  dof_publish_favorable_judgement_date   VARCHAR,
  created_at                             TIMESTAMP DEFAULT NOW(),
  updated_at                             TIMESTAMP
);
CREATE INDEX IF NOT EXISTS ix_efos_rfc ON public.efos(rfc);
CREATE INDEX IF NOT EXISTS ix_efos_name ON public.efos(name);
CREATE INDEX IF NOT EXISTS ix_efos_state ON public.efos(state);

-- CFDI catalogs (code/name pattern)
DO $$
DECLARE
  tbl TEXT;
BEGIN
  FOREACH tbl IN ARRAY ARRAY[
    'cat_aduana','cat_clave_prod_serv','cat_clave_unidad','cat_exportacion',
    'cat_forma_pago','cat_impuesto','cat_meses','cat_metodo_pago',
    'cat_moneda','cat_objeto_imp','cat_pais','cat_periodicidad',
    'cat_regimen_fiscal','cat_tipo_de_comprobante','cat_tipo_relacion','cat_uso_cfdi'
  ] LOOP
    EXECUTE format('
      CREATE TABLE IF NOT EXISTS public.%I (
        id         SERIAL PRIMARY KEY,
        identifier UUID UNIQUE,
        code       VARCHAR NOT NULL UNIQUE,
        name       VARCHAR NOT NULL
      )', tbl);
    EXECUTE format('CREATE INDEX IF NOT EXISTS ix_%s_name ON public.%I(name)', tbl, tbl);
  END LOOP;
END $$;

-- Nomina catalogs (code PK pattern)
DO $$
DECLARE
  tbl TEXT;
BEGIN
  FOREACH tbl IN ARRAY ARRAY[
    'cat_nom_tipo_nomina','cat_nom_tipo_contrato','cat_nom_tipo_jornada',
    'cat_nom_tipo_regimen','cat_nom_riesgo_puesto','cat_nom_periodicidad_pago',
    'cat_nom_banco','cat_nom_clave_ent_fed'
  ] LOOP
    EXECUTE format('
      CREATE TABLE IF NOT EXISTS public.%I (
        code VARCHAR PRIMARY KEY,
        name VARCHAR
      )', tbl);
  END LOOP;
END $$;
