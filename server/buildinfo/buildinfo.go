// Package buildinfo identifies binaries produced by the same build invocation.
package buildinfo

// ID is replaced by mage at link time for server/launcher compatibility checks.
var ID = "development"

// Version is the Dark Spinner semantic version displayed by bundled services.
// Direct Go builds use the fallback; Mage replaces it together with the
// launcher version for distributed desktop builds.
var Version = "0.5.0"
