package station

import (
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
	"example.com/paleo-prep-station/internal/domain"
	"example.com/paleo-prep-station/internal/repository"
)

// operatorName normalizes the caller identity carried by a write request. An
// empty name stays a validation failure, matching the previous behaviour of
// the recorded name fields.
func operatorName(field, value string) (string, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return "", contracts.Invalid("required_field", field+"不能为空")
	}
	return cleaned, nil
}

// authorize resolves the caller inside the current repository mutation and
// requires the role the action needs. Because the lookup shares the mutation
// lock with the business write, a concurrent role change and the guarded
// action always settle in one deterministic order.
func authorize(m *repository.Mutation, field, value string, want domain.Role) (string, error) {
	name, err := operatorName(field, value)
	if err != nil {
		return "", err
	}
	role, ok := m.OperatorRole(name)
	if !ok {
		return "", contracts.Forbidden("unknown_operator", "操作人 "+name+" 未登记职责，不能执行该操作")
	}
	if role != want {
		return "", contracts.Forbidden(
			"role_not_permitted",
			"操作人 "+name+" 的当前职责不能执行该操作，需要 "+string(want)+" 职责",
		)
	}
	return name, nil
}
