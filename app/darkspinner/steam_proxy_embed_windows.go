//go:build windows && steamproxy

package main

import _ "embed"

//go:embed steamproxy.dll
var embeddedSteamProxy []byte
