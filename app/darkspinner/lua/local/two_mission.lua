---Two seeded clients entering the same 1-1 mission through scoped UI operations.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", source_login_name = normalProfile.login_name, is_source_clone_allowed = true,
    }))
    local normal = must(normalProfile:Launch({client_id = "normal", launch_mode = "normal"}))
    local detached = must(detachedProfile:Launch({client_id = "detached", launch_mode = "detached"}))
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))

    must(normal.Party:Create("local-mission-party"))
    must(normal.Party:Invite(detached))
    must(detached.Party:Join(normal))
    must(normal.Scene:Set(scene.Navigation))
    must(normal.UI:Screenshot("navigation"))
    must(normal.Mission:Select(1))
    must(normal.UI:Screenshot("mission-selection"))
    local started = must(normal.Mission:Start())
    local normalDeployment = started.observation
    local detachedDeployment = must(detached.Mission:WaitDeployment())
    if normalDeployment.state.game_id ~= detachedDeployment.state.game_id
        or normalDeployment.state.mission.label ~= "1-1"
        or detachedDeployment.state.mission.label ~= "1-1" then
        return "owned clients did not deploy together into mission 1-1"
    end
    must(normal.Mission:WaitDeploymentReady())
    must(detached.Mission:WaitDeploymentReady())
    must(normal.UI:Screenshot("deployment"))
    must(detached.UI:Screenshot("deployment"))
    -- Both blocking operations must run together: the party enters as a group.
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
        or detachedMission.state.mission.label ~= "1-1" then
        return "owned clients did not commit the same mission 1-1"
    end
    check(scenario.Sleep(1500)) -- Capture after the usual entry fade; not render proof.
    must(normal.UI:Screenshot("shared-dungeon"))
    must(detached.UI:Screenshot("shared-dungeon"))
    return nil
end
