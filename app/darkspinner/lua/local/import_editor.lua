---Import Editor reproduction. This uses its separate Arsenal entry, not Mod Edit.
---Screenshots and draw observations establish evidence; script success alone does
---not establish correct floor materials or painted creature rendering.
local must = scenario.Must
local check = scenario.Check

local function capture(client, label)
    check(scenario.Sleep(3500))
    local hierarchy, hierarchyError = client.UI:Hierarchy()
    must(client.UI:Screenshot(label))
    hierarchy = must(hierarchy, hierarchyError)
    if not hierarchy.is_complete then error("import editor hierarchy was truncated") end
    if not hierarchy.is_import_editor then error("regular Mod editor opened instead of Import Editor") end
    if not hierarchy.is_environment_registered then error("imported environment directory is not registered") end
    return hierarchy
end

local function clickBody(client, hierarchy)
    local selected = nil
    for _, node in ipairs(hierarchy.nodes) do
        if node.id == 0xfad0b001 and node.is_visible and node.is_enabled then
            if selected ~= nil then error("Body tab is ambiguous") end
            selected = node
        end
    end
    if selected == nil then error("Body tab is unavailable") end
    local bounds = selected.bounds
    if #bounds ~= 4 or bounds[3] <= bounds[1] or bounds[4] <= bounds[2] then
        error("Body tab has no usable observed bounds")
    end
    must(client.UI:Click(math.floor((bounds[1]+bounds[3])/2), math.floor((bounds[2]+bounds[4])/2)))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({is_modded=true}))
    local profile = must(server:CreateProfile({
        name="import-editor", hero_nouns={749013658,1667741389,3618328019}, chain_progression=1,
    }))
    local client = must(profile:Launch({client_id="import-editor",launch_mode="normal",is_modded=true}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.UI:OpenImportEditor(749013658))
    local initial = capture(client,"import-editor-entry")
    clickBody(client,initial)
    local body = capture(client,"import-editor-body")
    if not body.is_body_mode then error("Body tab did not produce observed Body selection") end
    must(client.Diagnostics:Capture("import-editor-ground"))
    check(scenario.Log("Import Editor captured; review screenshots and scenario_editor_draw records. Client retained."))
end
