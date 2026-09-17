// Package assets exposes the default server configuration.
package assets

import _ "embed"

// DefaultConfigTOML is the documented configuration written on first start.
//
//go:embed darkspin.toml
var DefaultConfigTOML []byte
