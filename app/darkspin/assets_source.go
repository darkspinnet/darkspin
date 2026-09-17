//go:build windows && !frontend

package main

import "embed"

// frontendAssets lets Go tooling compile and test the launcher without retaining
// generated production assets. Official Mage builds use assets_frontend.go.
//
//go:embed frontend/index.html
var frontendAssets embed.FS
