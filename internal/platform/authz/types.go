package authz

// Ancestor is one parent fact on the path from a resource to its tenant. The
// guard that loads the resource supplies the full chain; the authorizer never
// infers ancestry from tenant-wide membership.
type Ancestor struct {
	Type string
	ID   string
}

// ResourceFact is the minimal, trusted resource description needed for one
// decision. It is built from a tenant-scoped row after authentication, never
// from request parameters alone.
type ResourceFact struct {
	Type      string
	ID        string
	TenantID  string
	Ancestors []Ancestor
	Status    string
	OwnerID   string
}

// Request is the only input a module may pass to the authorization decision
// point. Roles, tenant, and user are read from the trusted request context.
type Request struct {
	Action   Action
	Resource ResourceFact
}

// PolicyFacts records which relationship and which Cedar policies contributed
// to a decision. It is safe to expose through Explain and audit.
type PolicyFacts struct {
	FGAModelID     string
	FGARelation    string
	FGADecision    string
	CedarPolicyIDs []string
	CedarDecision  string
	BundleDigest   string
}

// Decision is the result of one authorization evaluation. A denied decision is
// a normal result; an error means the decision point could not decide and the
// request must fail closed.
type Decision struct {
	Allow      bool
	DecisionID string
	Reason     Reason
	Action     Action
	Resource   ResourceFact
	Policy     PolicyFacts
}
