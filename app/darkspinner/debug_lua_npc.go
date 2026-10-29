//go:build scenario

package main

import (
	"math"

	"github.com/yuin/gopher-lua"
)

// Identity is copied out of the returned table so editing InitialSnapshot cannot
// retarget a handle to an arbitrary NPC or a later process/game generation.
type debugLuaNPC struct {
	client              *debugLuaClient
	initialState        *lua.LTable
	instanceID          string
	spawnRequestID      string
	pid                 uint32
	userID              int64
	gameID              uint32
	peerGeneration      uint64
	transportGeneration uint64
	zoneGeneration      uint64
	objectID            uint32
}

func registerDebugLuaNPC(state *lua.LState) {
	meta := state.NewTypeMetatable("ControlledNPC")
	state.SetField(meta, "__index", state.NewFunction(debugLuaNPCIndex))
	state.SetField(meta, "__metatable", lua.LString("ControlledNPC"))
}

func debugLuaWorldSpawnControlled(state *lua.LState) int {
	return debugLuaWorldSpawnControlledRole(state, "client.spawn_controlled_npc")
}

func debugLuaWorldSpawnFixture(state *lua.LState) int {
	return debugLuaWorldSpawnControlledRole(state, "client.spawn_controlled_fixture")
}

func debugLuaWorldSpawnControlledRole(state *lua.LState, operation string) int {
	scope := debugLuaScopeSelf(state)
	if scope == nil {
		return 0
	}
	count := scope.client.invoke(state, operation, map[string]any{
		"client_id": scope.client.id, "noun": state.CheckString(2), "timeout_ms": debugLuaClientTimeout(state, scope.client, 3),
	}, true)
	request := debugLuaPendingYield(state, count)
	if request != nil {
		request.npcClient = scope.client
		return count
	}
	if count != 2 {
		return count
	}
	if state.Get(-1) != lua.LNil {
		failure := *debugLuaCause(state.Get(-1))
		return debugLuaNPCSpawnFailure(state, scope.client.vm, state.Get(-2), &failure)
	}
	snapshot, isTable := state.Get(-2).(*lua.LTable)
	if !isTable {
		return debugLuaNPCSpawnFailure(state, scope.client.vm, state.Get(-2), &debugError{Code: "invalid_result", Context: "npcHandle", Message: "spawn did not return a published NPC receipt"})
	}
	resource, failure := scope.client.vm.npcHandle(state, scope.client, snapshot)
	if failure != nil {
		return debugLuaNPCSpawnFailure(state, scope.client.vm, snapshot, failure)
	}
	state.Replace(-2, resource)
	return count
}

func (e *debugLuaVM) npcHandle(state *lua.LState, client *debugLuaClient, snapshot *lua.LTable) (lua.LValue, *debugError) {
	npc := &debugLuaNPC{client: client, initialState: snapshot}
	instanceID, isInstance := snapshot.RawGetString("instance_id").(lua.LString)
	requestID, isRequest := snapshot.RawGetString("request_id").(lua.LString)
	isValid := isInstance && instanceID != "" && isRequest && requestID != "" && snapshot.RawGetString("is_published") == lua.LTrue && snapshot.RawGetString("is_ai_enabled") == lua.LFalse
	npc.instanceID = string(instanceID)
	npc.spawnRequestID = string(requestID)
	fields := []string{"pid", "user_id", "game_id", "peer_generation", "transport_generation", "zone_generation", "object_id"}
	identities := make([]uint64, 0, len(fields))
	for _, field := range fields {
		number, isNumber := snapshot.RawGetString(field).(lua.LNumber)
		identity := float64(number)
		if !isNumber || math.IsNaN(identity) || math.IsInf(identity, 0) || identity < 1 || identity > 9007199254740991 || math.Trunc(identity) != identity {
			isValid = false
		}
		identities = append(identities, uint64(identity))
	}
	if !isValid || identities[0] > math.MaxUint32 || identities[2] > math.MaxUint32 || identities[6] > math.MaxUint32 {
		return lua.LNil, &debugError{Code: "invalid_result", Context: "npcIdentity", Message: "published spawn lacks an exact AI-disabled owned NPC identity"}
	}
	npc.pid, npc.userID, npc.gameID = uint32(identities[0]), int64(identities[1]), uint32(identities[2])
	npc.peerGeneration, npc.transportGeneration, npc.zoneGeneration, npc.objectID = identities[3], identities[4], identities[5], uint32(identities[6])
	userdata := state.NewUserData()
	userdata.Value = npc
	state.SetMetatable(userdata, state.GetTypeMetatable("ControlledNPC"))
	return userdata, nil
}

func debugLuaNPCSpawnFailure(state *lua.LState, vm *debugLuaVM, receipt lua.LValue, failure *debugError) int {
	partial, err := debugLuaJSON(receipt, 0)
	if err != nil {
		failure.Cause = &debugError{Code: "invalid_result", Context: "npcPartial", Message: err.Error(), Cause: failure.Cause}
	} else if partial != nil {
		failure.Details = map[string]any{"spawn_receipt": partial}
	}
	return vm.returnOperation(state, nil, failure, true)
}

func debugLuaNPCSelf(state *lua.LState) *debugLuaNPC {
	userdata := state.CheckUserData(1)
	npc, isNPC := userdata.Value.(*debugLuaNPC)
	if !isNPC || npc.client == nil {
		state.ArgError(1, "controlled NPC handle required")
		return nil
	}
	return npc
}

func debugLuaNPCIndex(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	switch state.CheckString(2) {
	case "Snapshot":
		state.Push(state.NewFunction(debugLuaNPCState))
	case "SetAI":
		state.Push(state.NewFunction(debugLuaNPCSetAI))
	case "Move":
		state.Push(state.NewFunction(debugLuaNPCMove))
	case "Cast":
		state.Push(state.NewFunction(debugLuaNPCCast))
	case "Restore":
		state.Push(state.NewFunction(debugLuaNPCRestore))
	case "Defeat":
		state.Push(state.NewFunction(debugLuaNPCDefeat))
	case "Retire":
		state.Push(state.NewFunction(debugLuaNPCRetire))
	case "ObjectID":
		state.Push(lua.LNumber(npc.objectID))
	case "InitialSnapshot":
		state.Push(npc.initialState)
	default:
		state.Push(lua.LNil)
	}
	return 1
}

func (e *debugLuaNPC) request() map[string]any {
	return map[string]any{"client_id": e.client.id, "instance_id": e.instanceID, "pid": e.pid, "user_id": e.userID,
		"game_id": e.gameID, "peer_generation": e.peerGeneration, "transport_generation": e.transportGeneration,
		"zone_generation": e.zoneGeneration, "spawn_request_id": e.spawnRequestID, "object_id": e.objectID}
}

func debugLuaNPCState(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	return npc.client.invoke(state, "client.controlled_npc_state", npc.request(), true)
}

func debugLuaNPCRestore(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	return npc.client.invoke(state, "client.controlled_npc_restore", npc.request(), true)
}

func debugLuaNPCDefeat(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	return npc.client.invoke(state, "client.controlled_npc_defeat", npc.request(), true)
}

func debugLuaNPCRetire(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	return npc.client.invoke(state, "client.controlled_npc_retire", npc.request(), true)
}

func debugLuaNPCSetAI(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	req := npc.request()
	req["is_ai_enabled"] = state.CheckBool(2)
	return npc.client.invoke(state, "client.controlled_npc_set_ai", req, true)
}

func debugLuaNPCMove(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	position, err := debugLuaJSON(state.CheckTable(2), 0)
	if err != nil {
		return npc.client.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "npcPosition", Message: err.Error()}, true)
	}
	req := npc.request()
	req["destination"] = position
	return npc.client.invoke(state, "client.controlled_npc_move", req, true)
}

func debugLuaNPCCast(state *lua.LState) int {
	npc := debugLuaNPCSelf(state)
	if npc == nil {
		return 0
	}
	target := float64(state.CheckNumber(3))
	if math.IsNaN(target) || math.IsInf(target, 0) || math.Trunc(target) != target || target < 1 || target > math.MaxUint32 {
		return npc.client.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "npcTarget", Message: "target_object_id must be a positive uint32 integer"}, true)
	}
	req := npc.request()
	req["ability_name"], req["target_object_id"] = state.CheckString(2), uint32(target)
	if state.GetTop() >= 4 && state.Get(4) != lua.LNil {
		timeout := float64(state.CheckNumber(4))
		if math.IsNaN(timeout) || math.IsInf(timeout, 0) || math.Trunc(timeout) != timeout || timeout < 1 || timeout > 120000 {
			return npc.client.vm.returnOperation(state, nil, &debugError{Code: "invalid_request", Context: "npcTimeout", Message: "timeout_ms must be an integer from 1 to 120000"}, true)
		}
		req["timeout_ms"] = uint32(timeout)
	}
	return npc.client.invoke(state, "client.controlled_npc_cast", req, true)
}
