---Two independent owned clients exercising ordinary eligible ship transitions.
---Scene receipts combine client reports and native UI eligibility; they
---do not establish a rendered scene, enabled gameplay controls or world entry.
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

    must(normal.Scene:Set(scene.Navigation))
    must(normal.UI:Screenshot("navigation-report"))
    must(detached.Scene:Set(scene.Editor))
    must(detached.UI:Screenshot("editor-report"))

    must(normal.Scene:Set(scene.Arsenal))
    must(normal.UI:Screenshot("arsenal-return-report"))
    -- Requests the ordinary Cancel button; an unsaved prompt is not auto-answered.
    must(detached.Scene:Set(scene.Arsenal))
    must(detached.UI:Screenshot("arsenal-return-report"))
    return nil
end
