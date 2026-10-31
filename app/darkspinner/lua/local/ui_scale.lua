-- Capture native layout and images for the same fresh profile recipe at two scales.
local must = scenario.Must
local check = scenario.Check

local function capture(server, percent)
    local name = "ui-scale-" .. percent
    local profile = must(server:CreateProfile({name = name,
        hero_nouns = {749013658, 1667741389, 3618328019}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = name, native_ui_scale_percent = percent,
        screen_width = 1920, screen_height = 1200, is_borderless_mode = true}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    check(scenario.Sleep(2500)) -- Allow animation to settle for visual inspection.
    must(client.UI:Screenshot(name .. "-arsenal"))
    must(client.UI:Click(35, 30))
    check(scenario.Sleep(500))
    must(client.UI:Screenshot(name .. "-portrait-menu"))
    must(client.UI:Click(700, 100))
    must(client.Scene:Set(scene.Editor))
    must(client.Scene:Wait(scene.Editor))
    check(scenario.Sleep(2500))
    must(client.UI:Hierarchy())
    must(client.UI:Screenshot(name .. "-editor"))
    must(client.Diagnostics:Capture(name))
    return client
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local baseline = capture(server, 100)
    check(baseline:Close())
    capture(server, 200)
end
