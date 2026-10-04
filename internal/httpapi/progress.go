package httpapi

import (
	"net/http"

	"example.com/paleo-prep-station/internal/contracts"
)

func (api *API) logProgress(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFromRequest(w, r)
	if !ok {
		return
	}
	var request contracts.ProgressRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	value, err := api.service.LogProgress(r.Context(), actor, r.PathValue("id"), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
