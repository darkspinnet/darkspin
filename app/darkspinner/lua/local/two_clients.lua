---Complete seeded normal/detached flow: reset, scenes, party and shared mission.
---Leaves both clients available for endpoint inspection after success or failure.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local originalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local normalProfile = must(server:ResetProfile({
        name = "normal", hero_nouns = {3618328019, 749013658, 1667741389}, chain_progression = 1,
    }))
    if normalProfile.user_id == originalProfile.user_id then
        return "profile reset reused the original account"
    end
    local detachedProfile = must(server:CreateProfile({
        name = "detached", source_login_name = normalProfile.login_name, is_source_clone_allowed = true,
    }))
    local normal = must(normalProfile:Launch({client_id = "normal", launch_mode = "normal"}))
    local detached = must(detachedProfile:Launch({client_id = "detached", launch_mode = "detached"}))
    must(normal:Login())
    must(detached:Login())
    local normalScene = must(normal.Scene:Wait(scene.Arsenal))
    local detachedScene = must(detached.Scene:Wait(scene.Arsenal))
    local normalState = normalScene.state
    local detachedState = detachedScene.state
    if normalState == nil or detachedState == nil then
        return "Arsenal readiness omitted its account observation"
    end
    if normalState.user_id ~= normalProfile.user_id
        or detachedState.user_id ~= detachedProfile.user_id then
        return "accepted login does not match its seeded profile"
    end
    must(normal.UI:Screenshot("seeded-arsenal"))
    must(detached.UI:Screenshot("seeded-arsenal"))

    must(normal.Scene:Set(scene.Navigation))
    must(normal.UI:Screenshot("navigation"))
    must(normal.Scene:Set(scene.Arsenal))
    must(detached.Scene:Set(scene.Editor))
    must(detached.UI:Screenshot("editor"))
    -- Uses ordinary Cancel; an unsaved-edit prompt remains for inspection.
    must(detached.Scene:Set(scene.Arsenal))

    must(normal.Party:Create("two-client-party"))
    must(normal.Party:Invite(detached))
    must(detached.Party:Join(normal))
    local normalParty = must(normal.Party:Snapshot())
    local detachedParty = must(detached.Party:Snapshot())
    if normalParty.party.id == 0 or normalParty.party.id ~= detachedParty.party.id
        or #normalParty.party.members ~= 2 or #detachedParty.party.members ~= 2 then
        return "owned clients do not observe the same two-member party"
    end
    must(normal.Scene:Set(scene.Navigation))
    must(normal.Mission:Select(1))
    must(normal.Mission:Start())
    must(detached.Mission:WaitDeployment())
    check(scenario.Parallel({
        normal = function()
            must(normal.Mission:Continue())
            return nil
        end,
        detached = function()
            must(detached.Mission:Continue())
            return nil
        end,
    }))
    local normalMission = must(normal.Mission:Wait())
    local detachedMission = must(detached.Mission:Wait())
    if normalMission.state.game_id ~= detachedMission.state.game_id
        or normalMission.state.mission.label ~= "1-1"
        or detachedMission.state.mission.label ~= "1-1"
        or normalMission.state.user_id ~= normalProfile.user_id
        or detachedMission.state.user_id ~= detachedProfile.user_id then
        return "seeded participants did not commit the same mission 1-1"
    end
    -- A deployment milestone remains satisfied after the group enters dungeon.
    local normalDeployment = must(normal.Mission:WaitDeployment())
    local detachedDeployment = must(detached.Mission:WaitDeployment())
    if normalDeployment.state.game_id ~= normalMission.state.game_id
        or detachedDeployment.state.game_id ~= normalMission.state.game_id then
        return "deployment milestone changed the committed game identity"
    end
    check(scenario.Sleep(1500)) -- Capture allowance, not a rendering assertion.
    must(normal.Diagnostics:Capture("shared-dungeon"))
    must(detached.Diagnostics:Capture("shared-dungeon"))
    return nil
end
