//go:build scenario

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/darkspinnet/darkspin/server/raknet"
	"github.com/darkspinnet/darkspin/server/scenario"
)

// A native receive observation followed by two owner frames separates deferred
// destruction from recreation of the same server object ID on every client.
type debugHeroRetirementWitness struct {
	owner         debugWorldOwner
	epoch         scenario.HeroResetRequest
	offset        int64
	deletedFrame  uint32
	ownerThreadID uint32
	isDeleted     bool
	isSettled     bool
}

func prepareDebugHeroRetirement(ctx context.Context, clientManager *debugClientManager, participants []debugHeroResetParticipant) ([]debugHeroRetirementWitness, error) {
	witnesses := make([]debugHeroRetirementWitness, 0, len(participants))
	for _, participant := range participants {
		owner := participant.owner
		fi, err := os.Stat(owner.client.TracePath)
		if err != nil {
			return nil, fmt.Errorf("retireTrace: %w", err)
		}
		probe, native, err := observeDebugHeroNative(ctx, clientManager, owner)
		if err != nil {
			return nil, fmt.Errorf("retireNative: %w", err)
		}
		if probe == nil || native == nil {
			return nil, errors.New("hero retirement requires current native owners")
		}
		witnesses = append(witnesses, debugHeroRetirementWitness{owner: owner, epoch: participant.epoch, offset: fi.Size(), ownerThreadID: native.OwnerThreadID})
	}
	return witnesses, nil
}

func (e *debugHeroRetirementWitness) observeDelete(objectID uint32) error {
	r, err := os.Open(e.owner.client.TracePath)
	if err != nil {
		return fmt.Errorf("deleteOpen: %w", err)
	}
	payload := make([]byte, 512*1024)
	count, readErr := r.ReadAt(payload, e.offset)
	closeErr := r.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return fmt.Errorf("deleteRead: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("deleteClose: %w", closeErr)
	}
	end := bytes.LastIndexByte(payload[:count], '\n')
	if end < 0 {
		if count == len(payload) {
			return errors.New("native retirement trace line exceeded observation bound")
		}
		return nil
	}
	for _, line := range bytes.Split(payload[:end], []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Protocol      string `json:"protocol"`
			Kind          string `json:"kind"`
			MessageID     uint32 `json:"message_id"`
			FrameSequence uint32 `json:"frame_sequence"`
			PayloadHex    string `json:"payload_hex"`
		}
		err = json.Unmarshal(line, &event)
		if err != nil {
			return fmt.Errorf("deleteDecode: %w", err)
		}
		if event.Protocol != "client_raknet" || event.Kind != "application_receive" || event.MessageID != uint32(raknet.ObjectDelete) {
			continue
		}
		packet, decodeErr := hex.DecodeString(event.PayloadHex)
		if decodeErr != nil {
			return fmt.Errorf("deletePacket: %w", decodeErr)
		}
		if len(packet) < 5 || packet[0] != byte(raknet.ObjectDelete) || (len(packet)-1)%4 != 0 || event.FrameSequence == 0 {
			return errors.New("native object deletion observation has an invalid shape")
		}
		for index := 1; index < len(packet); index += 4 {
			if binary.LittleEndian.Uint32(packet[index:index+4]) == objectID {
				e.isDeleted, e.deletedFrame = true, event.FrameSequence
			}
		}
	}
	e.offset += int64(end + 1)
	return nil
}

func waitDebugHeroRetirement(ctx context.Context, host *debugHost, clientManager *debugClientManager, witnesses []debugHeroRetirementWitness, objectID uint32) error {
	waitContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		settledCount := 0
		for index := range witnesses {
			witness := &witnesses[index]
			err := recheckDebugWorldOwner(waitContext, host, clientManager, witness.owner)
			if err != nil {
				return fmt.Errorf("deleteOwner: %w", err)
			}
			err = recheckDebugHeroResetEpoch(waitContext, host, witness.owner, witness.epoch)
			if err != nil {
				return fmt.Errorf("deleteEpoch: %w", err)
			}
			if witness.isSettled {
				settledCount++
				continue
			}
			err = witness.observeDelete(objectID)
			if err != nil {
				return fmt.Errorf("deleteObserve: %w", err)
			}
			if !witness.isDeleted {
				continue
			}
			probe, err := clientManager.InspectGameplay(waitContext, witness.owner.client.ClientID)
			if err != nil {
				return fmt.Errorf("deleteFrame: %w", err)
			}
			receipt, isReceipt := probe.(map[string]any)
			if !isReceipt || receipt["instance_id"] != witness.owner.client.InstanceID || receipt["pid"] != witness.owner.client.ProcessID {
				return errors.New("hero deletion observation changed its owned process")
			}
			native, isNative := receipt["gameplay_state"].(*controlGameplayState)
			if !isNative || native == nil || receipt["is_processed"] != true || !native.IsAvailable || native.OwnerThreadID != witness.ownerThreadID {
				return errors.New("hero deletion observation lost its native frame owner")
			}
			frameCount := native.FrameSequence - witness.deletedFrame
			if frameCount >= 2 && frameCount < 1<<31 {
				witness.isSettled = true
				settledCount++
			}
		}
		if settledCount == len(witnesses) {
			return nil
		}
		select {
		case <-waitContext.Done():
			return fmt.Errorf("deleteWait: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func waitDebugHeroReplacementRig(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner, command scenario.HeroResetRequest) error {
	waitContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var previousFrame uint32
	var previousHandle uint32
	for {
		err := recheckDebugWorldOwner(waitContext, host, clientManager, owner)
		if err != nil {
			return fmt.Errorf("rigOwner: %w", err)
		}
		err = recheckDebugHeroResetEpoch(waitContext, host, owner, command)
		if err != nil {
			return fmt.Errorf("rigEpoch: %w", err)
		}
		probe, native, nativeErr := observeDebugHeroNative(waitContext, clientManager, owner)
		if nativeErr != nil && !isDebugHeroUnavailable(nativeErr) {
			return fmt.Errorf("rigNative: %w", nativeErr)
		}
		if nativeErr == nil && probe != nil && native != nil && native.IsRigObserved && native.IsRigReady {
			if previousHandle == *native.ControlledHandle && native.FrameSequence > previousFrame {
				world, worldErr := host.server.DebugWorldState(waitContext, owner.client.UserID, owner.profile.GameID)
				if worldErr != nil {
					return fmt.Errorf("rigWorld: %w", worldErr)
				}
				if !isDebugHeroResetEpochCurrent(world, command) {
					return errors.New("hero replacement rig observation changed gameplay epochs")
				}
				if world.IsAvailable && world.Hero != nil && world.Hero.Noun == command.Noun &&
					world.Hero.ObjectID == command.ObjectID && world.Hero.Index == command.ObservedIndex {
					return nil
				}
			}
			previousHandle, previousFrame = *native.ControlledHandle, native.FrameSequence
		} else {
			previousHandle, previousFrame = 0, 0
		}
		select {
		case <-waitContext.Done():
			return fmt.Errorf("rigWait: %w", waitContext.Err())
		case <-ticker.C:
		}
	}
}

func refreshDebugHeroReplacement(ctx context.Context, host *debugHost, clientManager *debugClientManager, owner debugWorldOwner, command scenario.HeroResetRequest) error {
	world, err := host.server.DebugWorldState(ctx, owner.client.UserID, owner.profile.GameID)
	if err != nil {
		return fmt.Errorf("refreshWorld: %w", err)
	}
	if !world.IsAvailable || world.Hero == nil || world.Arena == nil && world.Campaign == nil {
		return errors.New("replacement refresh requires its current live world hero")
	}
	if world.Hero.ObjectID != command.ObjectID || world.Hero.Index != command.ObservedIndex || world.Hero.Noun != command.Noun {
		return errors.New("replacement refresh actor changed before native deployment")
	}
	err = recheckDebugHeroResetEpoch(ctx, host, owner, command)
	if err != nil {
		return fmt.Errorf("refreshEpoch: %w", err)
	}
	originalIndex := world.Hero.Index
	reserveIndex := uint32(3)
	characters := debugHeroReplacementCharacters(world, uint64(owner.client.UserID))
	for _, hero := range characters {
		if hero.Index != originalIndex && !hero.IsDefeated && hero.HitPoint > 0 {
			reserveIndex = hero.Index
			break
		}
	}
	if reserveIndex >= 3 {
		return errors.New("native replacement refresh requires a living reserve hero")
	}
	for _, slot := range []uint32{reserveIndex, originalIndex} {
		current, observeErr := host.server.DebugWorldState(ctx, owner.client.UserID, owner.profile.GameID)
		if observeErr != nil {
			return fmt.Errorf("refreshActor: %w", observeErr)
		}
		if !current.IsAvailable || current.Hero == nil {
			return errors.New("replacement refresh lost its current hero")
		}
		err = recheckDebugHeroResetEpoch(ctx, host, owner, command)
		if err != nil {
			return fmt.Errorf("refreshStepEpoch: %w", err)
		}
		reset := command
		reset.Noun, reset.Position = 0, nil
		reset.IsRetirement, reset.IsCooldownReset = false, true
		reset.ObjectID = current.Hero.ObjectID
		reset.ObservedNoun, reset.ObservedIndex = current.Hero.Noun, current.Hero.Index
		receipt, resetErr := host.server.DebugResetHero(ctx, reset)
		if resetErr != nil {
			return fmt.Errorf("refreshCooldown: %w", resetErr)
		}
		if !receipt.IsCooldownReset || !receipt.IsQueued {
			return errors.New("replacement refresh did not queue cooldown reset")
		}
		payload, marshalErr := json.Marshal(debugHeroSelectionRequest{ClientID: owner.client.ClientID, Slot: &slot, TimeoutMS: 20000})
		if marshalErr != nil {
			return fmt.Errorf("refreshRequest: %w", marshalErr)
		}
		selected, selectErr := selectDebugHero(ctx, host, clientManager, payload)
		if selectErr != nil {
			return fmt.Errorf("refreshSelect: %w", selectErr)
		}
		if selected.World.Hero == nil || selected.World.Hero.Index != slot {
			return errors.New("replacement refresh did not observe the selected hero")
		}
	}
	return nil
}

func debugHeroReplacementCharacters(world scenario.WorldState, userID uint64) []scenario.WorldHero {
	if world.Arena != nil {
		for _, member := range world.Arena.Members {
			if member.UserID == userID {
				return member.Characters
			}
		}
	}
	if world.Campaign != nil {
		for _, member := range world.Campaign.Members {
			if member.UserID == userID && member.IsReady {
				return member.Characters
			}
		}
	}
	return nil
}
