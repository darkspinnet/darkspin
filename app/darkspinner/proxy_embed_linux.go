//go:build linux && proxy

package main

import _ "embed"

//go:embed fangproxy.dll
var embeddedProxy []byte
