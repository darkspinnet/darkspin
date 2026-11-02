---Repeat a seeded two-client party mission twice on one owned server.
---Each pass has fresh accounts and process IDs; captures keep separate client IDs.
local must = scenario.Must
local check = scenario.Check

---@param normal ManagedClient
---@param detached ManagedClient
---@return MissionObservation normalMission
---@return MissionObservation detachedMission
local function enterMission(normal, detached)
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))
    must(normal.Party:Create("repeat-mission-party"))
    must(normal.Party:Invite(detached))
    must(detached.Party:Join(normal))
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
        or detachedMission.state.mission.label ~= "1-1" then
        error("repeat participants did not enter the same mission 1-1")
    end
    check(scenario.Sleep(1500)) -- Capture allowance, not native rendering proof.
    must(normal.UI:Screenshot("shared-dungeon"))
    must(detached.UI:Screenshot("shared-dungeon"))
    return normalMission, detachedMission
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", source_login_name = normalProfile.login_name, is_source_clone_allowed = true,
    }))
    for pass = 1, 2 do
        if pass > 1 then
            local previousNormalID = normalProfile.user_id
            local previousDetachedID = detachedProfile.user_id
            normalProfile = must(server:ResetProfile({
                name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
            }))
            detachedProfile = must(server:ResetProfile({
                name = "detached", source_login_name = normalProfile.login_name, is_source_clone_allowed = true,
            }))
            if normalProfile.user_id == previousNormalID or detachedProfile.user_id == previousDetachedID then
                return "repeat reset reused a previous account"
            end
        end
        local normal = must(normalProfile:Launch({
            client_id = "normal-" .. pass, launch_mode = "normal",
        }))
        local detached = must(detachedProfile:Launch({
            client_id = "detached-" .. pass, launch_mode = "detached",
        }))
        local normalMission, detachedMission = enterMission(normal, detached)
        if normalMission.state.user_id ~= normalProfile.user_id
            or detachedMission.state.user_id ~= detachedProfile.user_id then
            return "repeat mission used a different seeded profile"
        end
        -- Explicit process teardown avoids leaving an in-game quit prompt open.
        check(detached:Terminate())
        check(normal:Terminate())
        if must(normal:Snapshot()).is_running or must(detached:Snapshot()).is_running then
            return "repeat clients did not exit before profile reset"
        end
    end
    return nil
end
