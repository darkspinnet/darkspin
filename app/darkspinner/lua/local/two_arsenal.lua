---Two local clients, with an existing isolated seed profile as the clone source.
---Wait on client-authored presence rather than a fixed delay or screenshot.
---Presence does not prove enabled controls, a rendered scene, or world readiness.
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
    local normal = must(normalProfile:Launch({
        client_id = "normal", launch_mode = "normal",
    }))
    local detached = must(detachedProfile:Launch({
        client_id = "detached", launch_mode = "detached",
    }))
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))
    must(normal.UI:InspectWindow())
    must(detached.UI:InspectWindow())
    must(normal.UI:Screenshot("arsenal"))
    must(detached.UI:Screenshot("arsenal"))
    return nil
end
