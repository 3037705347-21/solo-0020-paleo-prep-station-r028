package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) releaseSpecimen(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var request contracts.ReleaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.ReleaseSpecimen(r.Context(), actor, r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (api *API) revokeRelease(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var request contracts.RevokeReleaseRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.RevokeRelease(r.Context(), actor, r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
