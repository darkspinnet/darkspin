package main

import (
	"encoding/json"

	serverauth "github.com/darkspinnet/darkspin/server/auth"
)

type verifyRequest struct {
	Token    string `json:"token"`
	Secret   string `json:"secret"`
	Issuer   string `json:"issuer"`
	Audience string `json:"audience"`
}

type verifyResponse struct {
	Identity string `json:"identity,omitempty"`
	Error    string `json:"error,omitempty"`
}

func verifyJSON(contents string) string {
	request := verifyRequest{}
	err := json.Unmarshal([]byte(contents), &request)
	if err != nil {
		return marshalResponse(verifyResponse{Error: "requestParse: " + err.Error()})
	}

	verifier, err := serverauth.NewJWTVerifier(
		[]byte(request.Secret),
		request.Issuer,
		request.Audience,
	)
	if err != nil {
		return marshalResponse(verifyResponse{Error: "verifierCreate: " + err.Error()})
	}

	identity, err := verifier.Verify(request.Token)
	if err != nil {
		return marshalResponse(verifyResponse{Error: "tokenVerify: " + err.Error()})
	}

	return marshalResponse(verifyResponse{Identity: identity})
}

func marshalResponse(response verifyResponse) string {
	contents, err := json.Marshal(response)
	if err != nil {
		return `{"error":"responseMarshal"}`
	}
	return string(contents)
}
