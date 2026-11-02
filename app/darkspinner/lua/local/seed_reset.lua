---Seed independent squads, replace one profile alias with a fresh account,
---then launch normal/detached clients and observe their seeded Arsenal scenes.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local originalProfile = must(server:CreateProfile({
        name = "normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "detached", hero_nouns = {749013658, 1667741389, 3618328019}, chain_progression = 1,
    }))
    -- Reset deliberately keeps the alias while allocating another account.
    -- An already running client using that alias must first be closed.
    local normalProfile = must(server:ResetProfile({
        name = "normal", hero_nouns = {3618328019, 749013658, 1667741389}, chain_progression = 1,
    }))
    if normalProfile.user_id == originalProfile.user_id then
        return "profile reset reused the original account"
    end
    local normal = must(normalProfile:Launch({
        client_id = "normal", launch_mode = "normal",
    }))
    local detached = must(detachedProfile:Launch({
        client_id = "detached", launch_mode = "detached",
    }))
    must(normal:Login())
    must(detached:Login())
    local normalScene = must(normal.Scene:Wait(scene.Arsenal))
    local detachedScene = must(detached.Scene:Wait(scene.Arsenal))
    if normalScene.state.user_id ~= normalProfile.user_id
        or detachedScene.state.user_id ~= detachedProfile.user_id then
        return "accepted client login does not match its seeded profile"
    end
    must(normal.UI:Screenshot("seeded"))
    must(detached.UI:Screenshot("seeded"))
    return nil
end
