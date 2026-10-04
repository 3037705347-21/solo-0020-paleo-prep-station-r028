package station

import (
	"strings"

	"example.com/paleo-prep-station/internal/contracts"
)

// requireRole enforces the duty boundary for an action. It is evaluated per
// request from the presented actor, so a role change takes effect immediately
// and concurrent requests are authorized independently before they serialize
// on the repository mutation.
func requireRole(actor contracts.Actor, role contracts.Role, action string) error {
	if actor.Role != role {
		return contracts.Forbidden("role_not_permitted", "当前调用者角色无权"+action)
	}
	return nil
}

// recordedName reconciles the legacy name field carried by a request body
// with the authenticated caller. An empty field defaults to the caller; a
// conflicting value is rejected so no request can attribute an action to
// someone else.
func recordedName(actor contracts.Actor, claimed string) (string, error) {
	cleaned := strings.TrimSpace(claimed)
	if cleaned == "" {
		return actor.Name, nil
	}
	if cleaned != actor.Name {
		return "", contracts.Forbidden("actor_mismatch", "请求中的姓名与调用者身份不一致")
	}
	return cleaned, nil
}
