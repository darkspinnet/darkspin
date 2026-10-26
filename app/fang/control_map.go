//go:build windows && cgo && scenario && fangdebug

package main

/*
#include "control.h"
*/
import "C"

type controlMapState struct {
	IsAvailable              bool    `json:"is_available"`
	IsLoaded                 bool    `json:"is_loaded"`
	IsCampaignReady          bool    `json:"is_campaign_ready"`
	IsPreviousReady          bool    `json:"is_previous_ready"`
	IsNextReady              bool    `json:"is_next_ready"`
	State                    *uint32 `json:"state"`
	Difficulty               *uint32 `json:"difficulty"`
	DifficultyMode           *uint32 `json:"difficulty_mode"`
	GameType                 *uint32 `json:"game_type"`
	PVPGameSize              *uint32 `json:"pvp_game_size"`
	PVPRankType              *uint32 `json:"pvp_rank_type"`
	PartyMemberCount         *uint32 `json:"party_member_count"`
	IsPVPUnlocked            *bool   `json:"is_pvp_unlocked"`
	IsPartyLeader            *bool   `json:"is_party_leader"`
	ArePVPMembersReady       *bool   `json:"are_pvp_members_ready"`
	AnimationState           *uint32 `json:"animation_state"`
	AnimationDifficulty      *uint32 `json:"animation_difficulty"`
	IsTransactionsProcessing *bool   `json:"is_transactions_processing"`
	ArePartyMembersReady     *bool   `json:"are_party_members_ready"`
	IsStartDisabled          *bool   `json:"is_start_disabled"`
	IsStartVisible           *bool   `json:"is_start_visible"`
}

type controlDeploymentState struct {
	IsAvailable             bool    `json:"is_available"`
	IsLoaded                bool    `json:"is_loaded"`
	IsReady                 bool    `json:"is_ready"`
	IsResultsScreen         bool    `json:"is_results_screen"`
	SquadIndex              *uint32 `json:"squad_index"`
	MajorDifficulty         *uint32 `json:"major_difficulty"`
	MinorDifficulty         *uint32 `json:"minor_difficulty"`
	IsSquadValid            *bool   `json:"is_squad_valid"`
	IsAcceptDisabled        *bool   `json:"is_accept_disabled"`
	IsAcceptVisible         *bool   `json:"is_accept_visible"`
	AreSquadButtonsDisabled *bool   `json:"are_squad_buttons_disabled"`
}

func readControlDeploymentReceipt(requestID uint32) *controlDeploymentState {
	var snapshot C.fang_control_deployment_state
	isFound := C.fang_control_window_deployment_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	fields := uint32(snapshot.fields)
	return &controlDeploymentState{
		IsAvailable: snapshot.is_available != 0, IsLoaded: snapshot.is_loaded != 0,
		IsReady: snapshot.is_ready != 0, IsResultsScreen: snapshot.is_results_screen != 0,
		SquadIndex:              controlMapNumber(fields, 1, uint32(snapshot.squad_index)),
		MajorDifficulty:         controlMapNumber(fields, 2, uint32(snapshot.major_difficulty)),
		MinorDifficulty:         controlMapNumber(fields, 4, uint32(snapshot.minor_difficulty)),
		IsSquadValid:            controlMapBoolean(fields, 8, uint32(snapshot.is_squad_valid)),
		IsAcceptDisabled:        controlMapBoolean(fields, 16, uint32(snapshot.is_accept_disabled)),
		IsAcceptVisible:         controlMapBoolean(fields, 32, uint32(snapshot.is_accept_visible)),
		AreSquadButtonsDisabled: controlMapBoolean(fields, 64, uint32(snapshot.are_squad_buttons_disabled)),
	}
}

func controlMapNumber(fields, bit, number uint32) *uint32 {
	if fields&bit == 0 {
		return nil
	}
	return &number
}

func controlMapBoolean(fields, bit, flag uint32) *bool {
	if fields&bit == 0 {
		return nil
	}
	isTrue := flag != 0
	return &isTrue
}

func readControlMapReceipt(requestID uint32) *controlMapState {
	var snapshot C.fang_control_map_state
	isFound := C.fang_control_window_map_result(C.uint32_t(requestID), &snapshot)
	if isFound == 0 {
		return nil
	}
	fields := uint32(snapshot.fields)
	return &controlMapState{
		IsAvailable: snapshot.is_available != 0, IsLoaded: snapshot.is_loaded != 0,
		IsCampaignReady:          snapshot.is_campaign_ready != 0,
		IsPreviousReady:          snapshot.is_previous_ready != 0,
		IsNextReady:              snapshot.is_next_ready != 0,
		State:                    controlMapNumber(fields, 1, uint32(snapshot.state)),
		Difficulty:               controlMapNumber(fields, 2, uint32(snapshot.difficulty)),
		DifficultyMode:           controlMapNumber(fields, 512, uint32(snapshot.difficulty_mode)),
		GameType:                 controlMapNumber(fields, 4, uint32(snapshot.game_type)),
		PVPGameSize:              controlMapNumber(fields, 1024, uint32(snapshot.pvp_game_size)),
		PVPRankType:              controlMapNumber(fields, 2048, uint32(snapshot.pvp_rank_type)),
		PartyMemberCount:         controlMapNumber(fields, 4096, uint32(snapshot.party_member_count)),
		IsPVPUnlocked:            controlMapBoolean(fields, 8192, uint32(snapshot.is_pvp_unlocked)),
		IsPartyLeader:            controlMapBoolean(fields, 16384, uint32(snapshot.is_party_leader)),
		ArePVPMembersReady:       controlMapBoolean(fields, 32768, uint32(snapshot.are_pvp_members_ready)),
		AnimationState:           controlMapNumber(fields, 8, uint32(snapshot.animation_state)),
		AnimationDifficulty:      controlMapNumber(fields, 16, uint32(snapshot.animation_difficulty)),
		IsTransactionsProcessing: controlMapBoolean(fields, 32, uint32(snapshot.is_transactions_processing)),
		ArePartyMembersReady:     controlMapBoolean(fields, 64, uint32(snapshot.are_party_members_ready)),
		IsStartDisabled:          controlMapBoolean(fields, 128, uint32(snapshot.is_start_disabled)),
		IsStartVisible:           controlMapBoolean(fields, 256, uint32(snapshot.is_start_visible)),
	}
}
