package domain

import (
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
)

// Role identifies the responsibility an operator carries when calling a write
// endpoint. Each operator name maps to exactly one role at a time, so a role
// change replaces the previous responsibility deterministically.
type Role string

const (
	// RoleRegistrar registers new specimens (登记员).
	RoleRegistrar Role = "registrar"
	// RolePrepLead drafts, approves and cancels preparation plans (制备负责人).
	RolePrepLead Role = "prep_lead"
	// RolePreparator records preparation progress (制备人员).
	RolePreparator Role = "preparator"
	// RoleCurator releases stabilized specimens for study (标本管理员).
	RoleCurator Role = "curator"
)

func (r Role) Valid() bool {
	switch r {
	case RoleRegistrar, RolePrepLead, RolePreparator, RoleCurator:
		return true
	default:
		return false
	}
}

func ParseRole(value string) (Role, error) {
	cleaned := strings.TrimSpace(value)
	role := Role(cleaned)
	if !role.Valid() {
		return "", contracts.Invalid(
			"unknown_role",
			"未知职责 "+cleaned+"，可用值：registrar、prep_lead、preparator、curator",
		)
	}
	return role, nil
}
