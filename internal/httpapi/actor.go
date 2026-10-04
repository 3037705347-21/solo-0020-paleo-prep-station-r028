package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

const (
	headerActorName = "X-Actor-Name"
	headerActorRole = "X-Actor-Role"
)

// actorFromRequest resolves the caller identity for write endpoints. Every
// mutating route must carry both headers; missing or unknown credentials are
// rejected before any business state is touched.
func actorFromRequest(w http.ResponseWriter, r *http.Request) (contracts.Actor, bool) {
	actor, err := contracts.NewActor(
		r.Header.Get(headerActorName),
		r.Header.Get(headerActorRole),
	)
	if err != nil {
		writeError(w, err)
		return contracts.Actor{}, false
	}
	return actor, true
}
