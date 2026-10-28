//go:build scenario

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func isIndependentDebugOperation(operation string) bool {
	switch operation {
	case "profile.create", "profile.logout", "client.launch", "client.close", "scenario.log":
		return true
	case "client.ui_hierarchy", "client.open_creature_editor":
		return true
	case "pvp.start":
		return true
	case "client.arena_lobby", "client.accept_arena", "client.wait_arena":
		return true
	case "client.screen_state", "client.set_screen", "client.wait_screen":
		return true
	case "client.type":
		return true
	case "client.ability_state", "client.cast_active", "client.cancel_ability":
		return true
	case "client.select_hero":
		return true
	case "client.restore_hero", "client.replace_hero", "client.place_hero", "client.reset_cooldowns", "client.set_ability_rank":
		return true
	case "client.debug_pause", "client.debug_resume", "client.debug_pause_state":
		return true
	case "client.beam_out":
		return true
	case "client.world_path":
		return true
	case "client.native_state", "client.project_world", "client.click_world", "client.hold_world":
		return true
	case "client.configure_world", "client.spawn_npc", "client.spawn_npc_async", "client.spawn_state":
		return true
	case "client.spawn_controlled_npc", "client.controlled_npc_state", "client.controlled_npc_set_ai", "client.controlled_npc_move", "client.controlled_npc_cast", "client.controlled_npc_restore", "client.controlled_npc_retire":
		return true
	case "client.world_state", "client.world_living", "client.npc_catalog", "client.wait_world_ready", "client.enter_mission":
		return true
	case "client.capture", "client.wait_signal":
		return true
	case "client.select_mission":
		return true
	case "client.deployment_state", "client.wait_deployment_ready":
		return true
	case "client.start_mission", "client.continue_mission":
		return true
	case "client.mission_selection", "client.wait_selection":
		return true
	case "scenario.sleep":
		return true
	case "profile.state", "profile.wait_login", "profile.wait_location", "client.state", "client.window", "client.screenshot", "client.click", "client.press", "client.ready", "client.wait_scene", "cinematic.configure", "cinematic.snapshot", "client.mission_state", "client.wait_mission", "client.wait_deployment", "client.set_scene", "party.state":
		return true
	default:
		return false
	}
}

// Session ownership keeps references alive until the helper joins. Lifecycle
// mutations remain serialized; independent client waits and loads overlap.
// Capture retains its per-client lock; each bridge admits one pending command.
func (e *debugSupervisor) executeIndependentDebugOperation(ctx context.Context, operation string, payload json.RawMessage) (any, error) {
	switch operation {
	case "profile.create", "profile.logout", "client.launch", "client.close", "scenario.log":
		result, err := e.executeSerializedDebugOperation(ctx, operation, payload)
		if err != nil {
			return result, fmt.Errorf("parallelLifecycle: %w", err)
		}
		return result, nil
	}
	err := e.lockDebugOperations(ctx)
	if err != nil {
		return nil, fmt.Errorf("independentLock: %w", err)
	}
	var host *debugHost
	var clientManager *debugClientManager
	if e.operationState != nil {
		host = e.operationState.host
		clientManager = e.operationState.clientManager
	}
	e.operationMutex.Unlock()
	switch operation {
	case "client.wait_signal":
		result, err := waitDebugObserverSignal(ctx, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("observerWait: %w", err)
		}
		return result, nil
	case "client.open_creature_editor":
		result, err := openDebugCreatureEditor(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("creatureEditor: %w", err)
		}
		return result, nil
	case "client.ui_hierarchy":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugClientRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("hierarchyRequest: %w", err)
		}
		result, err := clientManager.EditorHierarchy(ctx, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("hierarchyObserve: %w", err)
		}
		return result, nil
	case "client.arena_lobby", "client.accept_arena":
		result, err := executeDebugArena(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("arenaOperation: %w", err)
		}
		return result, nil
	case "client.wait_arena":
		result, err := waitDebugPVP(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("arenaWait: %w", err)
		}
		return result, nil
	case "pvp.start":
		result, err := startDebugPVP(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("pvpOperation: %w", err)
		}
		return result, nil
	case "client.screen_state", "client.set_screen", "client.wait_screen":
		result, err := executeDebugScreen(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("screenOperation: %w", err)
		}
		return result, nil
	case "client.ability_state":
		result, err := observeDebugAbility(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("abilityObserve: %w", err)
		}
		return result, nil
	case "client.cast_active":
		result, err := castDebugAbility(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("abilityCast: %w", err)
		}
		return result, nil
	case "client.cancel_ability":
		var req debugClientRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("cancelRequest: %w", err)
		}
		if req.ClientID == "" || req.IsTerminate {
			return nil, errors.New("ability cancellation requires only an owned client ID")
		}
		result, err := clientManager.CancelAbility(ctx, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("cancelNative: %w", err)
		}
		return result, nil
	case "client.restore_hero", "client.replace_hero", "client.place_hero", "client.reset_cooldowns", "client.set_ability_rank":
		result, err := resetDebugHero(ctx, host, clientManager, operation, payload)
		if err != nil {
			return debugHeroResetDTO(result, false), fmt.Errorf("heroFixture: %w", err)
		}
		return debugHeroResetDTO(result, result.IsReplacement), nil
	case "client.select_hero":
		result, err := selectDebugHero(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("heroSelect: %w", err)
		}
		return result, nil
	case "client.debug_pause", "client.debug_resume", "client.debug_pause_state":
		result, err := e.executeDebugPause(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("debuggerPause: %w", err)
		}
		return result, nil
	case "client.beam_out":
		result, err := beamOutDebugMission(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("missionBeamOut: %w", err)
		}
		return result, nil
	case "client.world_path":
		result, err := queryDebugWorldPath(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("worldPath: %w", err)
		}
		return result, nil
	case "client.native_state", "client.project_world", "client.click_world", "client.hold_world":
		result, err := executeDebugNative(ctx, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("nativeOperation: %w", err)
		}
		return result, nil
	case "client.configure_world":
		result, err := configureDebugWorld(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("worldConfigure: %w", err)
		}
		return result, nil
	case "client.spawn_npc", "client.spawn_npc_async", "client.spawn_state", "client.spawn_controlled_npc":
		result, err := executeDebugWorldSpawn(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("worldSpawn: %w", err)
		}
		return result, nil
	case "client.controlled_npc_state", "client.controlled_npc_set_ai", "client.controlled_npc_move", "client.controlled_npc_cast", "client.controlled_npc_restore", "client.controlled_npc_retire":
		result, err := executeDebugNPC(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("npcControl: %w", err)
		}
		return result, nil
	case "client.world_state":
		result, err := observeDebugWorld(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("worldObserve: %w", err)
		}
		return result, nil
	case "client.wait_world_ready":
		result, err := waitDebugWorldReady(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("worldReady: %w", err)
		}
		return result, nil
	case "client.world_living":
		result, err := observeDebugLivingWorld(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("worldLiving: %w", err)
		}
		return result, nil
	case "client.npc_catalog":
		result, err := readDebugNPCCatalog(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("npcCatalog: %w", err)
		}
		return result, nil
	case "client.enter_mission":
		result, err := enterDebugMission(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("missionEnter: %w", err)
		}
		return result, nil
	case "client.capture":
		result, err := e.captureDebugDiagnostic(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("clientCapture: %w", err)
		}
		return result, nil
	case "client.select_mission":
		result, err := selectDebugMission(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("missionSelect: %w", err)
		}
		return result, nil
	case "client.deployment_state", "client.wait_deployment_ready":
		result, err := observeDebugDeployment(ctx, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("deploymentInspect: %w", err)
		}
		return result, nil
	case "client.start_mission", "client.continue_mission":
		result, err := executeDebugMissionAction(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("missionAction: %w", err)
		}
		return result, nil
	case "client.mission_selection", "client.wait_selection":
		result, err := observeDebugMap(ctx, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("selectionObserve: %w", err)
		}
		return result, nil
	case "scenario.sleep":
		var req struct {
			DurationMS uint32 `json:"duration_ms"`
		}
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("sleepRequest: %w", err)
		}
		if req.DurationMS == 0 || req.DurationMS > 120000 {
			return nil, errors.New("sleep duration must be from 1 to 120000 milliseconds")
		}
		timer := time.NewTimer(time.Duration(req.DurationMS) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("sleepWait: %w", ctx.Err())
		case <-timer.C:
			return nil, nil
		}
	case "client.type":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugWindowTextRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("textRequest: %w", err)
		}
		result, err := clientManager.TypeWindow(ctx, req)
		if err != nil {
			return result, fmt.Errorf("clientType: %w", err)
		}
		return result, nil
	case "client.press":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugWindowPressRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("pressRequest: %w", err)
		}
		result, err := clientManager.PressWindow(ctx, req)
		if err != nil {
			return result, fmt.Errorf("clientPress: %w", err)
		}
		return result, nil
	case "party.state":
		result, err := executeDebugPartyOperation(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("partyObserve: %w", err)
		}
		return result, nil
	case "client.set_scene":
		result, err := setDebugClientScene(ctx, host, clientManager, payload)
		if err != nil {
			return result, fmt.Errorf("sceneSet: %w", err)
		}
		return result, nil
	case "client.mission_state", "client.wait_mission", "client.wait_deployment":
		result, err := observeDebugMission(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("missionObserve: %w", err)
		}
		return result, nil
	case "cinematic.snapshot":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugCinematicRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("cinematicDecode: %w", err)
		}
		result, err := clientManager.SnapshotCinematic(ctx, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("cinematicSnapshot: %w", err)
		}
		return result, nil
	case "cinematic.configure":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugCinematicRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("cinematicDecode: %w", err)
		}
		result, err := clientManager.ConfigureCinematic(ctx, req)
		if err != nil {
			return result, fmt.Errorf("cinematicConfigure: %w", err)
		}
		return result, nil
	case "client.ready", "client.wait_scene":
		result, err := waitDebugClientReady(ctx, host, clientManager, operation, payload)
		if err != nil {
			return result, fmt.Errorf("clientReadiness: %w", err)
		}
		return result, nil
	case "profile.state", "profile.wait_location":
		result, err := observeDebugProfile(ctx, host, operation, payload)
		if err != nil {
			return result, fmt.Errorf("profileState: %w", err)
		}
		return result, nil
	case "profile.wait_login":
		if host == nil {
			return nil, errors.New("start the isolated server before waiting for profile login")
		}
		var req debugLoginRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("loginRequest: %w", err)
		}
		result, err := waitDebugLogin(ctx, host, req)
		if err != nil {
			return result, fmt.Errorf("loginWait: %w", err)
		}
		return result, nil
	case "client.state":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugClientRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("stateRequest: %w", err)
		}
		client, err := clientManager.State(req.ClientID)
		if err != nil {
			return nil, fmt.Errorf("clientState: %w", err)
		}
		err = e.publishIndependentDebugClient(client)
		if err != nil {
			return nil, fmt.Errorf("statePublish: %w", err)
		}
		return debugOwnedClientResponse(client), nil
	case "client.screenshot":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugScreenshotRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("screenshotRequest: %w", err)
		}
		capture, captureErr := clientManager.Screenshot(ctx, req.ClientID, req.Label)
		result := map[string]any{"client_id": capture.ClientID, "instance_id": capture.InstanceID, "pid": capture.ProcessID, "is_available": capture.IsAvailable, "is_complete": capture.IsComplete, "reason": capture.Reason,
			"pixels": capture.Pixels, "pixels_sha256": capture.PixelsSHA256,
			"image_path": capture.ImagePath, "image_sha256": capture.ImageSHA256, "metadata_path": capture.MetadataPath, "metadata_sha256": capture.MetadataSHA256, "width": capture.Width, "height": capture.Height, "started_at": capture.StartedAt, "finished_at": capture.FinishedAt, "source": capture.Source}
		if captureErr != nil {
			return result, fmt.Errorf("clientScreenshot: %w", captureErr)
		}
		return result, nil
	case "client.window":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugClientRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("windowRequest: %w", err)
		}
		result, err := clientManager.InspectWindow(ctx, req.ClientID)
		if err != nil {
			return result, fmt.Errorf("clientWindow: %w", err)
		}
		return result, nil
	case "client.click":
		if clientManager == nil {
			return nil, errors.New("client manager is not initialized")
		}
		var req debugWindowClickRequest
		err := decodeDebugPayload(payload, &req)
		if err != nil {
			return nil, fmt.Errorf("clickRequest: %w", err)
		}
		result, err := clientManager.ClickWindow(ctx, req)
		if err != nil {
			return result, fmt.Errorf("clientClick: %w", err)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported independent operation %q", operation)
	}
}

func (e *debugSupervisor) publishIndependentDebugClient(client debugClientState) error {
	if client.Observation.Sequence == 0 {
		return nil
	}
	previous, err := e.registry.Client(client.ClientID)
	if err != nil {
		return fmt.Errorf("clientLookup: %w", err)
	}
	if client.Observation.Sequence <= previous.Sequence {
		return nil
	}
	err = e.registry.PublishState(client.Observation)
	if err == nil {
		return nil
	}
	// Another independent reader may have published the same or a newer frame
	// between lookup and publish. Only that exact owned identity supersedes us.
	current, lookupErr := e.registry.Client(client.ClientID)
	if lookupErr != nil {
		return fmt.Errorf("publishLookup: %w", errors.Join(err, lookupErr))
	}
	if current.Identity == client.Observation.Identity && current.Sequence >= client.Observation.Sequence {
		return nil
	}
	return fmt.Errorf("clientPublish: %w", err)
}
