---Two owned clients on explicit opposing teams using the production arena path.
---This prepares a server fixture; it does not test native matchmaking controls.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local firstProfile = must(server:CreateProfile({
        name = "pvp-first", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1, is_pvp_enabled = true,
    }))
    local secondProfile = must(server:CreateProfile({
        name = "pvp-second", source_login_name = firstProfile.login_name, is_source_clone_allowed = true, is_pvp_enabled = true,
    }))
    local firstClient = must(firstProfile:Launch({client_id = "pvp-first", launch_mode = "normal"}))
    local secondClient = must(secondProfile:Launch({client_id = "pvp-second", launch_mode = "detached"}))
    must(firstClient:Login())
    must(secondClient:Login())
    must(firstClient.Scene:Wait(scene.Arsenal))
    must(secondClient.Scene:Wait(scene.Arsenal))

    local match = must(server:StartPVP({teams = {{firstClient}, {secondClient}}}))
    if not match.is_mutation_accepted or not match.is_projection_complete or match.game_id == 0 then
        return "PvP formation or participant projection was not confirmed"
    end
    check(scenario.Parallel({
        first = function()
            must(firstClient.PVP:Accept())
            return nil
        end,
        second = function()
            must(secondClient.PVP:Accept())
            return nil
        end,
    }))
    local firstWorldSS = must(firstClient.PVP:Wait())
    local secondWorldSS = must(secondClient.PVP:Wait())
    if firstWorldSS.game_id ~= match.game_id or secondWorldSS.game_id ~= match.game_id then
        return "PvP participants did not load the formed arena together"
    end
    must(firstClient.Diagnostics:Capture("pvp-team-1-ready"))
    must(secondClient.Diagnostics:Capture("pvp-team-2-ready"))
    return nil
end
