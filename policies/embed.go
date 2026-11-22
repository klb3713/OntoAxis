// Package policies embeds the versioned authorization policy bundle.
//
// The bundle is loaded and validated at startup; it must not be edited at
// runtime. The OpenFGA model is uploaded by deployment, not by the core
// service.
package policies

import "embed"

// FS contains the OpenFGA model and the Cedar platform policies.
//
//go:embed openfga/model.fga platform/schema.cedar.json platform/*.cedar
var FS embed.FS
