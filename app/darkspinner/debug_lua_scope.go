//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

type debugLuaScope struct {
	client *debugLuaClient
	kind   string
}

func registerDebugLuaClientScopes(state *lua.LState) {
	for _, name := range []string{"ClientScene", "ClientUI", "ClientCinematic", "ClientMission", "ClientParty", "ClientDiagnostics", "ClientWorld", "ClientDebug", "ClientAbility", "ClientPVP"} {
		meta := state.NewTypeMetatable(name)
		state.SetField(meta, "__index", state.NewFunction(debugLuaScopeIndex))
		state.SetField(meta, "__metatable", lua.LString(name))
	}
	scenes := state.NewTable()
	scenes.RawSetString("Arsenal", lua.LString("arsenal"))
	scenes.RawSetString("Navigation", lua.LString("navigation"))
	scenes.RawSetString("Editor", lua.LString("editor"))
	state.SetGlobal("scene", scenes)
}

func debugLuaClientScope(state *lua.LState, client *debugLuaClient, kind string) *lua.LUserData {
	meta := "ClientUI"
	if kind == "scene" {
		meta = "ClientScene"
	} else if kind == "cinematic" {
		meta = "ClientCinematic"
	} else if kind == "mission" {
		meta = "ClientMission"
	} else if kind == "party" {
		meta = "ClientParty"
	} else if kind == "diagnostic" {
		meta = "ClientDiagnostics"
	} else if kind == "world" {
		meta = "ClientWorld"
	} else if kind == "debug" {
		meta = "ClientDebug"
	} else if kind == "ability" {
		meta = "ClientAbility"
	} else if kind == "screen" {
		meta = "ClientScreen"
	} else if kind == "pvp" {
		meta = "ClientPVP"
	}
	userdata := state.NewUserData()
	userdata.Value = &debugLuaScope{client: client, kind: kind}
	state.SetMetatable(userdata, state.GetTypeMetatable(meta))
	return userdata
}

func debugLuaScopeSelf(state *lua.LState) *debugLuaScope {
	userdata := state.CheckUserData(1)
	scope, isScope := userdata.Value.(*debugLuaScope)
	if !isScope || scope.client == nil {
		state.ArgError(1, "client scope required")
		return nil
	}
	return scope
}

func debugLuaScopeIndex(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	name := state.CheckString(2)
	if scope.kind == "pvp" {
		return debugLuaPVPIndex(state, name)
	}
	if scope.kind == "ability" {
		switch name {
		case "Snapshot":
			state.Push(state.NewFunction(debugLuaAbilityState))
		case "Cast":
			state.Push(state.NewFunction(debugLuaAbilityCast))
		case "Cancel":
			state.Push(state.NewFunction(debugLuaAbilityCancel))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "debug" {
		switch name {
		case "Pause":
			state.Push(state.NewFunction(debugLuaDebugPause))
		case "Resume":
			state.Push(state.NewFunction(debugLuaDebugResume))
		case "Snapshot":
			state.Push(state.NewFunction(debugLuaDebugPauseState))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "world" {
		switch name {
		case "RestoreHero":
			state.Push(state.NewFunction(debugLuaWorldRestoreHero))
		case "ResetCooldowns":
			state.Push(state.NewFunction(debugLuaWorldResetCooldowns))
		case "SetAbilityRank":
			state.Push(state.NewFunction(debugLuaWorldSetAbilityRank))
		case "PlaceHero":
			state.Push(state.NewFunction(debugLuaWorldPlaceHero))
		case "ReplaceHero":
			state.Push(state.NewFunction(debugLuaWorldReplaceHero))
		case "SelectHero":
			state.Push(state.NewFunction(debugLuaWorldSelectHero))
		case "Path":
			state.Push(state.NewFunction(debugLuaWorldPath))
		case "SnapshotNative", "NativeSnapshot":
			state.Push(state.NewFunction(debugLuaWorldNativeState))
		case "Project":
			state.Push(state.NewFunction(debugLuaWorldProject))
		case "Click":
			state.Push(state.NewFunction(debugLuaWorldClick))
		case "Hold":
			state.Push(state.NewFunction(debugLuaWorldHold))
		case "Configure":
			state.Push(state.NewFunction(debugLuaWorldConfigure))
		case "Spawn":
			state.Push(state.NewFunction(debugLuaWorldSpawn))
		case "SpawnOwnedNPC":
			state.Push(state.NewFunction(debugLuaWorldSpawnOwned))
		case "ActivateSpoffitCrystal":
			state.Push(state.NewFunction(debugLuaWorldActivateSpoffit))
		case "DeactivateSpoffitCrystal":
			state.Push(state.NewFunction(debugLuaWorldDeactivateSpoffit))
		case "SpawnControlledNPC", "SpawnControlled":
			state.Push(state.NewFunction(debugLuaWorldSpawnControlled))
		case "SpawnControlledFixture":
			state.Push(state.NewFunction(debugLuaWorldSpawnFixture))
		case "SpawnAsync":
			state.Push(state.NewFunction(debugLuaWorldSpawnAsync))
		case "SnapshotSpawn", "SpawnSnapshot":
			state.Push(state.NewFunction(debugLuaWorldSpawnState))
		case "Snapshot", "Status":
			state.Push(state.NewFunction(debugLuaWorldStatus))
		case "Ready":
			state.Push(state.NewFunction(debugLuaWorldReady))
		case "SnapshotLiving", "LivingSnapshot", "Living":
			state.Push(state.NewFunction(debugLuaWorldLiving))
		case "NPCCatalog":
			state.Push(state.NewFunction(debugLuaWorldNPCCatalog))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "diagnostic" {
		if name == "Capture" {
			state.Push(state.NewFunction(debugLuaDiagnosticCapture))
		} else if name == "WaitForSignal" {
			state.Push(state.NewFunction(debugLuaDiagnosticWaitForSignal))
		} else {
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "party" {
		return debugLuaPartyIndex(state, name)
	}
	if scope.kind == "mission" {
		switch name {
		case "BeamOut":
			state.Push(state.NewFunction(debugLuaMissionBeamOut))
		case "SnapshotDeployment", "Deployment":
			state.Push(state.NewFunction(debugLuaMissionDeployment))
		case "WaitDeploymentReady":
			state.Push(state.NewFunction(debugLuaMissionWaitDeploymentReady))
		case "SnapshotSelection", "Selection":
			state.Push(state.NewFunction(debugLuaMissionSelection))
		case "Select":
			state.Push(state.NewFunction(debugLuaMissionSelect))
		case "Enter":
			state.Push(state.NewFunction(debugLuaMissionEnter))
		case "WaitSelection":
			state.Push(state.NewFunction(debugLuaMissionWaitSelection))
		case "SnapshotMission", "Snapshot":
			state.Push(state.NewFunction(debugLuaMissionState))
		case "Wait":
			state.Push(state.NewFunction(debugLuaMissionWait))
		case "WaitDeployment":
			state.Push(state.NewFunction(debugLuaMissionWaitDeployment))
		case "Start":
			state.Push(state.NewFunction(debugLuaMissionStart))
		case "Continue":
			state.Push(state.NewFunction(debugLuaMissionContinue))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "cinematic" {
		switch name {
		case "Snapshot":
			state.Push(state.NewFunction(debugLuaCinematicSnapshot))
		case "SetSkipEnabled":
			state.Push(state.NewFunction(debugLuaCinematicSetSkip))
		case "SetIntroSkipEnabled":
			state.Push(state.NewFunction(debugLuaCinematicSetIntroSkip))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	if scope.kind == "scene" {
		switch name {
		case "Set":
			state.Push(state.NewFunction(debugLuaSceneSet))
		case "Wait":
			state.Push(state.NewFunction(debugLuaSceneWait))
		case "Snapshot":
			state.Push(state.NewFunction(debugLuaSceneState))
		default:
			state.Push(lua.LNil)
		}
		return 1
	}
	switch name {
	case "OpenCreatureEditor":
		state.Push(state.NewFunction(debugLuaUIOpenCreatureEditor))
	case "OpenImportEditor":
		state.Push(state.NewFunction(debugLuaUIOpenImportEditor))
	case "Hierarchy":
		state.Push(state.NewFunction(debugLuaUIHierarchy))
	case "Screen":
		if scope.client.screenScope == nil {
			scope.client.screenScope = debugLuaClientScope(state, scope.client, "screen")
		}
		state.Push(scope.client.screenScope)
	case "InspectWindow", "Window":
		state.Push(state.NewFunction(debugLuaUIWindow))
	case "Click":
		state.Push(state.NewFunction(debugLuaUIClick))
	case "Press":
		state.Push(state.NewFunction(debugLuaUIPress))
	case "Type":
		state.Push(state.NewFunction(debugLuaUIType))
	case "Screenshot":
		state.Push(state.NewFunction(debugLuaUIScreenshot))
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func debugLuaUIOpenCreatureEditor(state *lua.LState) int {
	return debugLuaUIOpenEditor(state, false)
}

func debugLuaUIOpenImportEditor(state *lua.LState) int {
	return debugLuaUIOpenEditor(state, true)
}

func debugLuaUIOpenEditor(state *lua.LState, isImport bool) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	heroNoun := state.CheckNumber(2)
	if heroNoun <= 0 || heroNoun > math.MaxUint32 || math.Trunc(float64(heroNoun)) != float64(heroNoun) {
		state.ArgError(2, "hero noun must be a positive uint32")
		return 0
	}
	return scope.client.invoke(state, "client.open_creature_editor", map[string]any{"client_id": scope.client.id,
		"hero_noun": uint32(heroNoun), "is_import_editor": isImport, "timeout_ms": debugLuaClientTimeout(state, scope.client, 3)}, true)
}

func debugLuaUIHierarchy(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.ui_hierarchy", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaUIPress(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.press", map[string]any{
		"client_id": scope.client.id, "key": state.CheckString(2),
	}, true)
}

func debugLuaUIType(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.type", map[string]any{
		"client_id": scope.client.id, "text": state.CheckString(2),
	}, true)
}

func debugLuaSceneSet(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.vm.invoke(state, "client.set_scene", map[string]any{
		"client_id": scope.client.id, "location": state.CheckString(2),
		"timeout_ms": debugLuaClientTimeout(state, scope.client, 3),
	}, true)
}

func debugLuaCinematicSetSkip(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "cinematic.configure", map[string]any{
		"client_id": scope.client.id, "is_cinematic_skipped": state.CheckBool(2),
	}, false)
}

func debugLuaCinematicSnapshot(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "cinematic.snapshot", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaCinematicSetIntroSkip(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "cinematic.configure", map[string]any{
		"client_id": scope.client.id, "is_intro_skipped": state.CheckBool(2),
	}, false)
}

func debugLuaSceneWait(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	revision := float64(state.OptNumber(4, 0))
	if math.IsNaN(revision) || math.IsInf(revision, 0) || revision < 0 || revision > 9007199254740991 || math.Trunc(revision) != revision {
		return scope.client.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "presenceRevision", Message: "after revision must be a nonnegative exactly representable integer"}, true)
	}
	return scope.client.vm.invoke(state, "client.wait_scene", map[string]any{
		"client_id": scope.client.id, "location": state.CheckString(2),
		"timeout_ms":     debugLuaClientTimeout(state, scope.client, 3),
		"after_revision": revision,
	}, true)
}

func debugLuaSceneState(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "profile.state", map[string]any{"name": scope.client.profileName}, true)
}

func debugLuaUIWindow(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.window", map[string]any{"client_id": scope.client.id}, true)
}

func debugLuaUIClick(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.click", map[string]any{"client_id": scope.client.id, "x": state.CheckInt(2), "y": state.CheckInt(3), "button": state.OptString(4, "left")}, true)
}

func debugLuaUIScreenshot(state *lua.LState) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	return scope.client.invoke(state, "client.screenshot", map[string]any{"client_id": scope.client.id, "label": state.CheckString(2)}, true)
}
