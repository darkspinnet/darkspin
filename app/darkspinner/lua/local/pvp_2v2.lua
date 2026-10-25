---Four owned clients, with explicit teams and native arena-lobby acceptance.
local must = scenario.Must
local check = scenario.Check

local function launchParticipant(server, name, mode)
    local profile = must(server:CreateProfile({
        name = name, hero_nouns = {1667741389, 3618328019, 749013658}, is_pvp_enabled = true,
    }))
    local client = must(profile:Launch({client_id = name, launch_mode = mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local firstClient = launchParticipant(server, "pvp-first", "normal")
    local secondClient = launchParticipant(server, "pvp-second", "detached")
    local thirdClient = launchParticipant(server, "pvp-third", "detached")
    local fourthClient = launchParticipant(server, "pvp-fourth", "detached")
    local match = must(server:StartPVP({teams = {{firstClient, secondClient}, {thirdClient, fourthClient}}}))
    check(scenario.Parallel({
        first = function() must(firstClient.PVP:Accept()); return nil end,
        second = function() must(secondClient.PVP:Accept()); return nil end,
        third = function() must(thirdClient.PVP:Accept()); return nil end,
        fourth = function() must(fourthClient.PVP:Accept()); return nil end,
    }))
    for _, client in ipairs({firstClient, secondClient, thirdClient, fourthClient}) do
        local worldSS = must(client.PVP:Wait())
        if worldSS.game_id ~= match.game_id then
            return "PvP participant loaded a different arena"
        end
        must(client.Diagnostics:Capture("pvp-ready"))
    end
    return nil
end
