package domain

import (
	"time"

	"example.com/paleo-prep-station/internal/contracts"
)

type ReleaseRecord struct {
	ID           string     `json:"id"`
	SpecimenID   string     `json:"specimen_id"`
	Purpose      string     `json:"purpose"`
	Recipient    string     `json:"recipient"`
	ApprovedBy   string     `json:"approved_by"`
	Revoked      bool       `json:"revoked"`
	RevokedBy    string     `json:"revoked_by,omitempty"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func NewReleaseRecord(
	id string,
	specimen Specimen,
	purpose string,
	recipient string,
	approvedBy string,
	now time.Time,
) (ReleaseRecord, error) {
	if id == "" {
		return ReleaseRecord{}, contracts.Internal("missing_generated_id", "放行标识未生成", nil)
	}
	if !specimen.IsStable() {
		return ReleaseRecord{}, contracts.Conflict("specimen_not_releasable", "只有已稳定标本可以办理放行")
	}
	purposeValue, err := requiredText("借阅目的", purpose, 200)
	if err != nil {
		return ReleaseRecord{}, err
	}
	recipientValue, err := requiredText("接收方", recipient, 120)
	if err != nil {
		return ReleaseRecord{}, err
	}
	approver, err := requiredText("批准人", approvedBy, 80)
	if err != nil {
		return ReleaseRecord{}, err
	}
	return ReleaseRecord{
		ID:         id,
		SpecimenID: specimen.ID,
		Purpose:    purposeValue,
		Recipient:  recipientValue,
		ApprovedBy: approver,
		CreatedAt:  now.UTC(),
	}, nil
}

// Revoke marks the current release record as revoked while preserving the
// original record. A revoked record can never be revoked again.
func (r *ReleaseRecord) Revoke(revokedBy string, reason string, now time.Time) error {
	if r.Revoked {
		return contracts.Conflict("release_already_revoked", "放行记录已经撤销，不能重复撤销")
	}
	revoker, err := requiredText("撤销人", revokedBy, 80)
	if err != nil {
		return err
	}
	reasonValue, err := optionalText("撤销原因", reason, 200)
	if err != nil {
		return err
	}
	stamp := now.UTC()
	r.Revoked = true
	r.RevokedBy = revoker
	r.RevokedAt = &stamp
	r.RevokeReason = reasonValue
	return nil
}
