package control

import "github.com/uptrace/bun"

// CodeName-based catalogs: id (PK), identifier (UUID), code (unique), name.

type CatAduana struct {
	bun.BaseModel `bun:"table:cat_aduana"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatClaveProdServ struct {
	bun.BaseModel `bun:"table:cat_clave_prod_serv"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatClaveUnidad struct {
	bun.BaseModel `bun:"table:cat_clave_unidad"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatExportacion struct {
	bun.BaseModel `bun:"table:cat_exportacion"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatFormaPago struct {
	bun.BaseModel `bun:"table:cat_forma_pago"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatImpuesto struct {
	bun.BaseModel `bun:"table:cat_impuesto"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatMeses struct {
	bun.BaseModel `bun:"table:cat_meses"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatMetodoPago struct {
	bun.BaseModel `bun:"table:cat_metodo_pago"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatMoneda struct {
	bun.BaseModel `bun:"table:cat_moneda"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatObjetoImp struct {
	bun.BaseModel `bun:"table:cat_objeto_imp"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatPais struct {
	bun.BaseModel `bun:"table:cat_pais"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatPeriodicidad struct {
	bun.BaseModel `bun:"table:cat_periodicidad"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatRegimenFiscal struct {
	bun.BaseModel `bun:"table:cat_regimen_fiscal"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatTipoDeComprobante struct {
	bun.BaseModel `bun:"table:cat_tipo_de_comprobante"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatTipoRelacion struct {
	bun.BaseModel `bun:"table:cat_tipo_relacion"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

type CatUsoCFDI struct {
	bun.BaseModel `bun:"table:cat_uso_cfdi"`
	ID            int64  `bun:"id,pk,autoincrement" json:"id"`
	Identifier    string `bun:"identifier,type:uuid" json:"identifier"`
	Code          string `bun:"code,notnull,unique" json:"code"`
	Name          string `bun:"name,notnull" json:"name"`
}

// Nomina catalogs: code (PK), name.

type CatTipoNomina struct {
	bun.BaseModel `bun:"table:cat_nom_tipo_nomina"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatTipoContrato struct {
	bun.BaseModel `bun:"table:cat_nom_tipo_contrato"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatTipoJornada struct {
	bun.BaseModel `bun:"table:cat_nom_tipo_jornada"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatTipoRegimen struct {
	bun.BaseModel `bun:"table:cat_nom_tipo_regimen"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatRiesgoPuesto struct {
	bun.BaseModel `bun:"table:cat_nom_riesgo_puesto"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatPeriodicidadPago struct {
	bun.BaseModel `bun:"table:cat_nom_periodicidad_pago"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatBanco struct {
	bun.BaseModel `bun:"table:cat_nom_banco"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}

type CatClaveEntFed struct {
	bun.BaseModel `bun:"table:cat_nom_clave_ent_fed"`
	Code          string `bun:"code,pk,notnull" json:"code"`
	Name          string `bun:"name" json:"name"`
}
