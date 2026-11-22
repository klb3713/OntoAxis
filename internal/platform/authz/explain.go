package authz

import (
	"encoding/json"
	"io"
	"net/http"
)

// ExplainRequest is the admin-only explain payload. It contains facts, never
// roles or policy text.
type ExplainRequest struct {
	Action   string       `json:"action"`
	Resource ResourceFact `json:"resource"`
}

// ExplainResponse exposes the decision and matched policy identifiers only;
// policy bodies and attribute values are intentionally omitted.
type ExplainResponse struct {
	Allowed    bool        `json:"allowed"`
	Reason     string      `json:"reason,omitempty"`
	DecisionID string      `json:"decision_id"`
	Policy     PolicyFacts `json:"policy"`
}

// ExplainHandler serves POST /api/v1/authorization/explain. The authorizer
// enforces the admin-only and self-resource constraints; this adapter only
// performs protocol validation.
func ExplainHandler(authorizer *Authorizer) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body ExplainRequest
		decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			WriteHTTPError(w, r, &Error{Reason: ReasonAttributeMissing, Engine: "explain", Err: err})
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			WriteHTTPError(w, r, &Error{Reason: ReasonAttributeMissing, Engine: "explain"})
			return
		}
		decision, err := authorizer.Explain(r.Context(), Request{Action: Action(body.Action), Resource: body.Resource})
		if err != nil {
			WriteHTTPError(w, r, err)
			return
		}
		if decision.DecisionID != "" {
			w.Header().Set(DecisionHeader, decision.DecisionID)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(ExplainResponse{
			Allowed:    decision.Allow,
			Reason:     string(decision.Reason),
			DecisionID: decision.DecisionID,
			Policy:     decision.Policy,
		})
	})
}
