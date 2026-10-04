package contracts

import "strings"

// Role identifies the duty a caller claims for a write request. Roles are
// presented per request and evaluated at decision time, so a role change
// takes effect on the next request without any cached state.
type Role string

const (
	// RoleRegistrar registers new specimens (登记员).
	RoleRegistrar Role = "registrar"
	// RolePrepLead drafts prep plans and approves them (制备负责人).
	RolePrepLead Role = "prep_lead"
	// RolePreparator records preparation progress (制备人员).
	RolePreparator Role = "preparator"
	// RoleCurator releases stabilized specimens and revokes releases (标本管理员).
	RoleCurator Role = "curator"
)

const maxActorNameRunes = 80

// Actor is the authenticated caller identity attached to a write request.
type Actor struct {
	Name string
	Role Role
}

// ParseRole validates a raw role string against the known duty roles.
func ParseRole(value string) (Role, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", Forbidden("actor_role_required", "缺少调用者角色请求头")
	}
	switch Role(cleaned) {
	case RoleRegistrar, RolePrepLead, RolePreparator, RoleCurator:
		return Role(cleaned), nil
	default:
		return "", Forbidden("unknown_actor_role", "调用者角色不在已知职责范围内")
	}
}

// NewActor builds a caller identity from transport-provided credentials.
func NewActor(name string, roleValue string) (Actor, error) {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return Actor{}, Forbidden("actor_name_required", "缺少调用者姓名请求头")
	}
	if len([]rune(cleaned)) > maxActorNameRunes {
		return Actor{}, Invalid("invalid_actor_name", "调用者姓名长度超出限制")
	}
	role, err := ParseRole(roleValue)
	if err != nil {
		return Actor{}, err
	}
	return Actor{Name: cleaned, Role: role}, nil
}
