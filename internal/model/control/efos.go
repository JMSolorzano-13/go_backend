package control

import (
	"time"

	"github.com/uptrace/bun"
)

const (
	EFOSStateDefinitive       = "DEFINITIVE"
	EFOSStateDistorted        = "DISTORTED"
	EFOSStateAlleged          = "ALLEGED"
	EFOSStateFavorableJudgment = "FAVORABLE_JUDGMENT"
)

type EFOS struct {
	bun.BaseModel `bun:"table:efos,alias:e"`

	ID                                  int64      `bun:"id,pk,autoincrement" json:"id"`
	Identifier                          string     `bun:"identifier,type:uuid,notnull,unique" json:"identifier"`
	No                                  int64      `bun:"no,notnull" json:"no"`
	RFC                                 string     `bun:"rfc,notnull" json:"rfc"`
	Name                                string     `bun:"name,notnull" json:"name"`
	State                               string     `bun:"state,notnull" json:"state"`
	SATOfficeAlleged                    *string    `bun:"sat_office_alleged" json:"sat_office_alleged"`
	SATPublishAllegedDate               string     `bun:"sat_publish_alleged_date,notnull" json:"sat_publish_alleged_date"`
	DOFOfficeAlleged                    *string    `bun:"dof_office_alleged" json:"dof_office_alleged"`
	DOFPublishAllegedDate               *string    `bun:"dof_publish_alleged_date" json:"dof_publish_alleged_date"`
	SATOfficeDistored                   *string    `bun:"sat_office_distored" json:"sat_office_distored"`
	SATPublishDistoredDate              *string    `bun:"sat_publish_distored_date" json:"sat_publish_distored_date"`
	DOFOfficeDistored                   *string    `bun:"dof_office_distored" json:"dof_office_distored"`
	DOFPublishDistoredDate              *string    `bun:"dof_publish_distored_date" json:"dof_publish_distored_date"`
	SATOfficeDefinitive                 *string    `bun:"sat_office_definitive" json:"sat_office_definitive"`
	SATPublishDefinitiveDate            *string    `bun:"sat_publish_definitive_date" json:"sat_publish_definitive_date"`
	DOFOfficeDefinitive                 *string    `bun:"dof_office_definitive" json:"dof_office_definitive"`
	DOFPublishDefinitiveDate            *string    `bun:"dof_publish_definitive_date" json:"dof_publish_definitive_date"`
	SATOfficeFavorableJudgement         *string    `bun:"sat_office_favorable_judgement" json:"sat_office_favorable_judgement"`
	SATPublishFavorableJudgementDate    *string    `bun:"sat_publish_favorable_judgement_date" json:"sat_publish_favorable_judgement_date"`
	DOFOfficeFavorableJudgement         *string    `bun:"dof_office_favorable_judgement" json:"dof_office_favorable_judgement"`
	DOFPublishFavorableJudgementDate    *string    `bun:"dof_publish_favorable_judgement_date" json:"dof_publish_favorable_judgement_date"`
	CreatedAt                           time.Time  `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt                           *time.Time `bun:"updated_at" json:"updated_at"`
}
