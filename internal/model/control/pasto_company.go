package control

import (
	"time"

	"github.com/uptrace/bun"
)

type PastoCompany struct {
	bun.BaseModel `bun:"table:pasto_company,alias:pc"`

	PastoCompanyID      string     `bun:"pasto_company_id,pk,type:uuid,notnull" json:"pasto_company_id"`
	WorkspaceIdentifier string     `bun:"workspace_identifier,type:uuid,notnull" json:"workspace_identifier"`
	Name                string     `bun:"name,notnull" json:"name"`
	Alias               string     `bun:"alias,notnull" json:"alias"`
	RFC                 string     `bun:"rfc,notnull" json:"rfc"`
	BDD                 *string    `bun:"bdd,default:'Base de datos no identificada'" json:"bdd"`
	System              *string    `bun:"system,default:'Sistema no identificado'" json:"system"`
	CreatedAt           time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt           *time.Time `bun:"updated_at" json:"updated_at"`

	Workspace *Workspace `bun:"rel:belongs-to,join:workspace_identifier=identifier" json:"workspace,omitempty"`
}
