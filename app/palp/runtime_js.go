//go:build js && wasm

package main

import "syscall/js"

func run() {
	callback := js.FuncOf(verifyJWT)
	js.Global().Set("darkspinAuthVerifyJWT", callback)
	select {}
}

func verifyJWT(_ js.Value, arguments []js.Value) any {
	if len(arguments) != 1 {
		return marshalResponse(verifyResponse{Error: "expected one JSON request argument"})
	}
	if arguments[0].Type() != js.TypeString {
		return marshalResponse(verifyResponse{Error: "request argument must be a JSON string"})
	}
	return verifyJSON(arguments[0].String())
}
