---Mod creature-editor handoff: capture the initial editor and its Body palette.
---No layout parity assertions or creature edits are made by this starter.
local must = scenario.Must
local check = scenario.Check

---@param client ManagedClient
---@param label string
---@return EditorUIHierarchy
local function capture(client, label)
    must(client.Scene:Wait(scene.Editor))
    check(scenario.Sleep(3500)) -- Capture allowance, not a render-complete claim.
    local hierarchy, hierarchyError = client.UI:Hierarchy()
    must(client.UI:Screenshot(label))
    hierarchy = must(hierarchy, hierarchyError)
    if not hierarchy.is_complete then
        error("editor hierarchy was truncated; inspect saved artifact before asserting missing controls")
    end
    return hierarchy
end

---@param client ManagedClient
---@param hierarchy EditorUIHierarchy
---@param id integer
local function clickElement(client, hierarchy, id)
    local selected = nil
    for _, node in ipairs(hierarchy.nodes) do
        if node.id == id and node.is_visible and node.is_enabled then
            if selected ~= nil then
                error("editor element ID is ambiguous in the visible hierarchy")
            end
            selected = node
        end
    end
    if selected == nil then
        error("editor element was not visible and enabled in the captured hierarchy")
    end
    local bounds = selected.bounds
    if #bounds ~= 4 or bounds[3] <= bounds[1] or bounds[4] <= bounds[2] then
        error("editor element has no usable captured bounds")
    end
    -- Use fresh hierarchy bounds only within this unchanged editor instance.
    -- A processing receipt alone is not evidence that the tab changed.
    must(client.UI:Click(math.floor((bounds[1] + bounds[3]) / 2), math.floor((bounds[2] + bounds[4]) / 2)))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({is_modded = true}))
    local profile = must(server:CreateProfile({
        name = "mod-editor", hero_nouns = {749013658, 1667741389, 3618328019}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "mod-editor", launch_mode = "normal", is_modded = true}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.Scene:Set(scene.Editor))
    local native = capture(client, "native-editor-equip")
    clickElement(client, native, 0xf019c2f3)
    capture(client, "native-editor-paint")
    must(client.Scene:Set(scene.Arsenal))
    must(client.Scene:Wait(scene.Arsenal))
    must(client.UI:OpenCreatureEditor(749013658)) -- Explicit owned Sage, independent of desktop hover.
    local initial = capture(client, "mod-editor-equip")
    clickElement(client, initial, 0xfad0b001) -- Mod's native Body tab, not a guessed pixel.
    local body = capture(client, "mod-editor-body")
    if not body.is_body_mode then
        return "Body tab processing did not produce an observed Body selection; inspect saved hierarchy and screenshot"
    end
    local retained = capture(client, "mod-editor-body-retained")
    clickElement(client, retained, 0xfad0b002)
    local confirmation, confirmationError = client.UI:Hierarchy()
    must(client.UI:Screenshot("mod-editor-reset-confirm"))
    clickElement(client, must(confirmation, confirmationError), 0xfad0b002)
    capture(client, "mod-editor-reset-body")
    must(client.Diagnostics:Capture("mod-editor-handoff"))
    -- Leave the owned client in Body mode. The supervisor keeps its debug
    -- endpoint available after this script finishes for the next agent.
    return nil
end
