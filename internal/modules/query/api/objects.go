package api

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

func (h *Handler) queryObjects(w http.ResponseWriter, r *http.Request) {
	tc, ok := tenant(w, r)
	if !ok {
		return
	}
	var objectTypeID uuid.UUID
	if raw := r.URL.Query().Get("object_type_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httperr.Write(w, r, httperr.BadRequest("invalid object_type_id"))
			return
		}
		objectTypeID = id
	}
	page, perPage := pagination(r)
	// v1 authorizes the object list at tenant level, filtered or not:
	// query/api has no tenant-scoped ObjectType reader (the query service only
	// exposes QueryObjects), and synthesizing a fact from the client-supplied
	// object_type_id would authorize a resource the handler never loaded.
	// A future change can narrow the filtered form to an ObjectType fact once
	// such a tenant-scoped reader exists.
	if !h.authorize(w, r, objectRead, tenantFact(tc.TenantID)) {
		return
	}
	items, total, err := h.service.QueryObjects(r.Context(), tc.TenantID, objectTypeID, perPage, (page-1)*perPage)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "total": total, "page": page, "per_page": perPage})
}
