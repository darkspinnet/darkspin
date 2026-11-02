---Create two isolated profiles, then own a normal and existing detached client.
---This waits for actual server authentication from both launched clients.
---Party formation, shared mission entry and world readiness are not implied.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local normal = must(normalProfile:Launch({
        client_id = "normal", launch_mode = "normal",
    }))
    local detached = must(detachedProfile:Launch({
        client_id = "detached", launch_mode = "detached",
    }))
    must(normal:Login())
    must(detached:Login())
    must(normal:Snapshot())
    must(detached:Snapshot())
    return nil
end
