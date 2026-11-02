//go:build scenario

package main

type debugWindowPressRequest struct {
	ClientID string `json:"client_id"`
	Key      string `json:"key"`
}

type debugWindowTextRequest struct {
	ClientID string `json:"client_id"`
	Text     string `json:"text"`
}

type debugWindowClickRequest struct {
	ClientID string `json:"client_id"`
	X        int32  `json:"x"`
	Y        int32  `json:"y"`
	Button   string `json:"button,omitempty"`
}
