package sqlstore

import (
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/zone/checkpoint"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
)

// The checkpoint's existing fields retain their representation. Entrance
// persistence explicitly projects only its semantic progress and authored
// entrance operands; runtime clocks and combat ActionProfile data stay private.
type checkpointPayload struct {
	checkpoint.Snapshot
	NPCs                []npcPayload
	ObjectiveRuntimes   []objectiveRuntimePayload
	ObjectiveDrops      []questDropPayload
	QuestCrystals       []questCrystalPayload
	ObjectiveEvents     []questEventPayload
	ObjectiveNextDropID uint64
}

type npcPayload struct {
	State        npcStatePayload
	Introduction *introductionPayload `json:"Introduction,omitempty"`
	Mutation     *mutationPayload     `json:"Mutation,omitempty"`
}

// Mutation has an explicit sibling DTO. The shadow prevents its exported
// runtime fields from also being serialized through the legacy actor record.
type npcStatePayload struct {
	zonenpc.Snapshot
	Mutation *struct{} `json:"Mutation,omitempty"`
}

type introductionPayload struct {
	Elapsed            time.Duration
	SourcePosition     game.Vec3
	Generation         uint64
	Profile            introductionProfilePayload
	IsCommitted        bool
	IsComplete         bool
	IsEffectEmitted    bool
	IsAnimationEmitted bool
	IsBossActive       bool
	IsInitiallyHidden  bool
}

type introductionProfilePayload struct {
	FirstAggroAbilityName        string
	FirstAggroAnimationName      string
	FirstAggroAnimationDelay     time.Duration
	FirstAggroEffectName         string
	FirstAggroEffectDelay        time.Duration
	FirstAggroDelay              time.Duration
	FirstAggroRevealDelay        time.Duration
	FirstAggroCinematicDuration  time.Duration
	FirstAggroCinematicRadius    float32
	FirstAggroCinematicOffset    game.Vec3
	IsFirstAggroDurationKnown    bool
	IsFirstAggroFacingSuppressed bool
	PreAggroAnimationName        string
}

func encodeCheckpoint(snapshot checkpoint.Snapshot) checkpointPayload {
	encoded := checkpointPayload{Snapshot: snapshot, NPCs: make([]npcPayload, 0, len(snapshot.NPCs))}
	encoded.ObjectiveRuntimes = encodeObjectiveRuntimes(snapshot.ObjectiveRuntimes)
	encodeQuestState(&encoded)
	encoded.ObjectiveNextDropID = snapshot.ObjectiveNextDropID
	for _, npc := range snapshot.NPCs {
		encoded.NPCs = append(encoded.NPCs, npcPayload{State: npcStatePayload{Snapshot: npc.State},
			Introduction: encodeIntroduction(npc.Introduction), Mutation: encodeMutation(npc.State.Mutation, npc.MutationCapturedAt)})
	}
	return encoded
}

func decodeCheckpoint(encoded checkpointPayload) checkpoint.Snapshot {
	snapshot := encoded.Snapshot
	snapshot.ObjectiveRuntimes = decodeObjectiveRuntimes(encoded.ObjectiveRuntimes)
	snapshot.ObjectiveDrops, snapshot.QuestCrystals, snapshot.ObjectiveEvents = decodeQuestState(encoded)
	snapshot.ObjectiveNextDropID = encoded.ObjectiveNextDropID
	snapshot.NPCs = make([]checkpoint.NPC, 0, len(encoded.NPCs))
	for _, npc := range encoded.NPCs {
		state := npc.State.Snapshot
		state.Mutation = decodeMutation(npc.Mutation, snapshot.SavedAt)
		snapshot.NPCs = append(snapshot.NPCs, checkpoint.NPC{State: state, Introduction: decodeIntroduction(npc.Introduction),
			MutationCapturedAt: snapshot.SavedAt})
	}
	return snapshot
}

func encodeIntroduction(state zonenpc.IntroductionState) *introductionPayload {
	if !state.IsSet {
		return nil
	}
	profile := state.Profile
	return &introductionPayload{
		Elapsed: state.Elapsed, Generation: state.Generation,
		SourcePosition: state.SourcePosition,
		IsCommitted:    state.IsCommitted, IsComplete: state.IsComplete,
		IsEffectEmitted: state.IsEffectEmitted, IsBossActive: state.IsBossActive,
		IsAnimationEmitted: state.IsAnimationEmitted,
		IsInitiallyHidden:  state.IsInitiallyHidden,
		Profile: introductionProfilePayload{
			FirstAggroAbilityName:        profile.FirstAggroAbilityName,
			FirstAggroAnimationName:      profile.FirstAggroAnimationName,
			FirstAggroAnimationDelay:     profile.FirstAggroAnimationDelay,
			FirstAggroEffectName:         profile.FirstAggroEffectName,
			FirstAggroEffectDelay:        profile.FirstAggroEffectDelay,
			FirstAggroDelay:              profile.FirstAggroDelay,
			FirstAggroRevealDelay:        profile.FirstAggroRevealDelay,
			FirstAggroCinematicDuration:  profile.FirstAggroCinematicDuration,
			FirstAggroCinematicRadius:    profile.FirstAggroCinematicRadius,
			FirstAggroCinematicOffset:    profile.FirstAggroCinematicOffset,
			IsFirstAggroDurationKnown:    profile.IsFirstAggroDurationKnown,
			IsFirstAggroFacingSuppressed: profile.IsFirstAggroFacingSuppressed,
			PreAggroAnimationName:        profile.PreAggroAnimationName,
		},
	}
}

func decodeIntroduction(encoded *introductionPayload) zonenpc.IntroductionState {
	if encoded == nil {
		return zonenpc.IntroductionState{}
	}
	profile := encoded.Profile
	return zonenpc.IntroductionState{
		Elapsed: encoded.Elapsed, Generation: encoded.Generation, IsSet: true,
		SourcePosition: encoded.SourcePosition,
		IsCommitted:    encoded.IsCommitted, IsComplete: encoded.IsComplete,
		IsEffectEmitted: encoded.IsEffectEmitted, IsBossActive: encoded.IsBossActive,
		IsAnimationEmitted: encoded.IsAnimationEmitted,
		IsInitiallyHidden:  encoded.IsInitiallyHidden,
		Profile: zonenpc.ActionProfile{
			FirstAggroAbilityName:        profile.FirstAggroAbilityName,
			FirstAggroAnimationName:      profile.FirstAggroAnimationName,
			FirstAggroAnimationDelay:     profile.FirstAggroAnimationDelay,
			FirstAggroEffectName:         profile.FirstAggroEffectName,
			FirstAggroEffectDelay:        profile.FirstAggroEffectDelay,
			FirstAggroDelay:              profile.FirstAggroDelay,
			FirstAggroRevealDelay:        profile.FirstAggroRevealDelay,
			FirstAggroCinematicDuration:  profile.FirstAggroCinematicDuration,
			FirstAggroCinematicRadius:    profile.FirstAggroCinematicRadius,
			FirstAggroCinematicOffset:    profile.FirstAggroCinematicOffset,
			IsFirstAggroDurationKnown:    profile.IsFirstAggroDurationKnown,
			IsFirstAggroFacingSuppressed: profile.IsFirstAggroFacingSuppressed,
			PreAggroAnimationName:        profile.PreAggroAnimationName,
		},
	}
}
