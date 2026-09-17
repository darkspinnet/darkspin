//go:build (linux || darwin) && proxy

package main

import _ "embed"

//go:embed fangproxy.dll
var embeddedProxy []byte
