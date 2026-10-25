---Top-level overlays are separate from the ship room reported by Scene.
---Native selection is observable; a settling delay is only a capture allowance.
local must = scenario.Must
local check = scenario.Check

---@param client ManagedClient
---@param name ScreenName
---@param label string
local function visit(client, name, label)
    local selected = must(client.UI.Screen:Set(name))
    if selected.screen_state == nil or not selected.screen_state.is_available
        or selected.screen_state.screen ~= name then
        return "screen selection did not report the intended native overlay: " .. label
    end
    must(client.UI.Screen:Wait(name))
    local observed = must(client.UI.Screen:Snapshot())
    if observed.screen_state == nil or observed.screen_state.screen ~= name then
        return "screen observation lost the selected overlay: " .. label
    end
    check(scenario.Sleep(1500)) -- Let opening animations settle before screenshots.
    must(client.Diagnostics:Capture("screen-" .. label))
    if name == screen.Inventory then
        local refused, refusal = client.UI.Screen:Set(screen.Profile)
        if refusal == nil or (refused ~= nil and refused.action ~= nil) then
            return "switching out of Inventory did not preserve the explicit close gate"
        end
    end
    local closed = must(client.UI.Screen:Close())
    if closed.screen_state == nil or closed.screen_state.screen ~= screen.None then
        return "screen close did not observe all supported overlays closed: " .. label
    end
    return nil
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "screens", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "screens", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.UI.Screen:Close())
    for _, entry in ipairs({
        {name = screen.Inventory, label = "inventory"},
        {name = screen.Galaxy, label = "galaxy"},
        {name = screen.Profile, label = "profile"},
        {name = screen.Friends, label = "friends"},
        {name = screen.Leaderboards, label = "leaderboards"},
        {name = screen.Manual, label = "manual"},
    }) do
        local failure = visit(client, entry.name, entry.label)
        if failure ~= nil then
            local capture, captureError = client.Diagnostics:Capture("screen-flow-failed")
            return failure .. "; diagnostic=" .. tostring(capture ~= nil and capture.manifest_path or captureError)
        end
    end
    must(client.Scene:Set(scene.Navigation))
    local failure = visit(client, screen.Profile, "navigation-profile")
    if failure ~= nil then
        return failure
    end
    must(client.Scene:Set(scene.Arsenal))
    check(scenario.Sleep(1500))
    must(client.Diagnostics:Capture("screen-flow-complete"))
    return nil
end
