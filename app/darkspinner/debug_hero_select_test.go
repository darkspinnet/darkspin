//go:build scenario

package main

import "testing"

func TestHeroSelectionWaitRequiresKnownRefusalAndSameActor(t *testing.T) {
	handle, previousSlot, eligibility := uint32(42), uint32(0), uint32(6)
	initial := &controlGameplayState{IsAvailable: true, IsWorldActive: true, ControlledHandle: &handle, OwnerThreadID: 7}
	selection := &controlHeroSelection{RequestedSlot: 1, PreviousControlledHandle: &handle,
		PreviousSlot: &previousSlot, EligibilityCode: &eligibility, IsQueryProcessed: true}
	receipt := map[string]any{"status": "unavailable", "is_invocation_attempted": false,
		"is_invocation_accepted": false, "hero_selection": selection, "gameplay_state": initial}
	if !isDebugHeroDispatchBlocked(receipt, 1, initial) {
		t.Fatal("known cooldown refusal did not permit waiting")
	}
	receipt["is_invocation_attempted"] = true
	if isDebugHeroDispatchBlocked(receipt, 1, initial) {
		t.Fatal("attempted input permitted replay")
	}
	receipt["is_invocation_attempted"] = false
	delete(receipt, "is_invocation_accepted")
	if isDebugHeroDispatchBlocked(receipt, 1, initial) {
		t.Fatal("missing dispatch evidence permitted replay")
	}
	receipt["is_invocation_accepted"] = false
	changedHandle := uint32(43)
	changed := *initial
	changed.ControlledHandle = &changedHandle
	receipt["gameplay_state"] = &changed
	if isDebugHeroDispatchBlocked(receipt, 1, initial) {
		t.Fatal("changed actor permitted replay")
	}
	receipt["gameplay_state"] = initial
	eligibility = 0
	if isDebugHeroDispatchBlocked(receipt, 1, initial) {
		t.Fatal("eligible query without dispatch was treated as a known cooldown refusal")
	}
}
