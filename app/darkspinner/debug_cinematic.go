//go:build scenario

package main

import "errors"

type debugCinematicRequest struct {
	ClientID           string `json:"client_id"`
	IsIntroSkipped     *bool  `json:"is_intro_skipped,omitempty"`
	IsCinematicSkipped *bool  `json:"is_cinematic_skipped,omitempty"`
}

func validateDebugCinematic(req debugCinematicRequest) error {
	if req.ClientID == "" || req.IsIntroSkipped == nil && req.IsCinematicSkipped == nil {
		return errors.New("cinematic configuration requires an owned client ID and at least one skip setting")
	}
	return nil
}
