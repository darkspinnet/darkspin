---Skip opening movies, but leave in-game cinematics available for review.
---Returning keeps this owned session available through the debug endpoint.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "review", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    -- Both skip switches default to true for scenario clients.
    local client = must(profile:Launch({client_id = "review", launch_mode = "normal"}))
    check(client:SetTimeout(40000))
    check(client.Cinematic:SetSkipEnabled(false))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.UI:Screenshot(string.format("arsenal-script-%d-client-%d-ms",
        math.floor(scenario.Time()), math.floor(client:Time()))))
    -- Before another transition, resume skipping with:
    -- check(client.Cinematic:SetSkipEnabled(true))
    -- Opening movies are independently controlled with:
    -- check(client.Cinematic:SetIntroSkipEnabled(false))
    return nil
end
