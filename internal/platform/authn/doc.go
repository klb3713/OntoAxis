// Package authn defines the trusted identity boundary for OntoAxis.
//
// The package owns the only writable TenantContext implementation. Business
// handlers must read identity through FromContext or RequireTenant and must
// never construct a TenantContext themselves. Authentication is fail-closed:
// missing, malformed, or unverifiable credentials are rejected before a
// handler runs.
package authn
