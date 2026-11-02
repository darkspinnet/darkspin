---Fresh ordinary solo campaign with an immutable server seed/population recipe.
---Uses a mission-1 captain named by the server's authored boss catalog. The
---installed director must actually contain it; unsupported catalogs fail.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "focused", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "focused", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Select(1))
    must(client.Mission:Start())
    must(client.Mission:Continue())
    local mission = must(client.Mission:Wait())
    if mission.state.user_id ~= profile.user_id or mission.state.mission.label ~= "1-1" then
        return "focused client entered an unexpected account or mission"
    end
    local receipt = must(client.World:Spawn("ZelemSpecialHaster_Captain.Noun"))
    if not receipt.is_published or receipt.object_id == 0 or receipt.is_native_observed then
        return "spawn omitted its server publication or claimed unsupported native visibility"
    end
    local worldSS = must(client.World:Snapshot())
    if not worldSS.is_available or worldSS.game_id ~= receipt.game_id
        or worldSS.peer_generation ~= receipt.peer_generation
        or worldSS.zone_generation ~= receipt.zone_generation then
        return "published spawn does not match the current owned world epoch"
    end
    must(client.Diagnostics:Capture("focused-published"))
    return nil
end
