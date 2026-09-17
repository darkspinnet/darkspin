//go:build !fang

package main

// Development and test builds may use an external Fang DLL. Official builds
// select fang_embed.go and carry the compiled DLL in darkspinner.exe.
var embeddedFang []byte
