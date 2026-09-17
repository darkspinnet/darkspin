//go:build fang

package main

import _ "embed"

//go:embed fang.dll
var embeddedFang []byte
