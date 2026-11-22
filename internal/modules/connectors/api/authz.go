package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/klb3713/ontoaxis/internal/modules/connectors/domain"
	"github.com/klb3713/ontoaxis/internal/platform/authz"
	"github.com/klb3713/ontoaxis/internal/platform/httperr"
)

// Resource type names used when building authorization facts. They must match
// the resource types registered in the platform authorization schema.
const (
	resourceTypeTenant    = "tenant"
	resourceTypeConnector = "connector"
)

// authorize delegates one decision to the platform guard. A nil guard allows
// the request, preserving the pre-authorization behavior for callers that have
// not wired a guard yet.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, action authz.Action, resource authz.ResourceFact) bool {
	return authz.AuthorizeHTTP(w, r, h.guard, action, resource)
}

// tenantFact builds the tenant-level authorization fact used by connector
// creation and tenant-level connector lists.
func tenantFact(tenantID string) authz.ResourceFact {
	return authz.ResourceFact{Type: resourceTypeTenant, ID: tenantID, TenantID: tenantID}
}

// connectorFact builds the authorization fact for a connector loaded
// tenant-scoped. The stored tenant from the loaded row is authoritative, and
// the lifecycle status is included so resource-state rules can apply (for
// example, sync is only allowed for active connectors).
func connectorFact(connector *domain.Connector) authz.ResourceFact {
	return authz.ResourceFact{
		Type:     resourceTypeConnector,
		ID:       connector.ID,
		TenantID: connector.TenantID,
		Status:   string(connector.Status),
	}
}

// loadConnectorForAuthorization loads the connector row tenant-scoped before an
// authorization decision and any side effect. A missing or cross-tenant
// connector is a 404 that never reaches the authorization layer.
func (h *Handler) loadConnectorForAuthorization(w http.ResponseWriter, r *http.Request, tenantID string) (*domain.Connector, bool) {
	connector, err := h.svc.GetConnector(r.Context(), tenantID, chi.URLParam(r, "connectorID"))
	if err != nil {
		writeServiceError(w, r, err)
		return nil, false
	}
	if connector == nil {
		writeServiceError(w, r, domain.ErrNotFound)
		return nil, false
	}
	if connector.TenantID != tenantID {
		httperr.Write(w, r, httperr.NotFound("connector not found"))
		return nil, false
	}
	return connector, true
}
