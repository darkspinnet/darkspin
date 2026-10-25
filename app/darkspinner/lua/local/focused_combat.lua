---Focused real-client basic-attack probe. Uses ordinary native targeting and
---requires server-observed damage and death; publication is not visibility.
local must = scenario.Must
local check = scenario.Check

local function findTarget(worldSS, objectID)
    for _, npc in ipairs(worldSS.npcs) do
        if npc.object_id == objectID then
            return npc
        end
    end
    return nil
end

---@param client ManagedClient
---@param target WorldNPC
---@return string|nil
local function attackTarget(client, target)
    local receipt, actionError = client.World:Hold(target.position, 1000)
    if actionError ~= nil then
        return "native action stopped without replay: " .. tostring(actionError)
    end
    if receipt == nil or not receipt.is_processed or receipt.gameplay_click == nil
        or not receipt.gameplay_click.is_press_poll_observed
        or not receipt.gameplay_click.is_release_poll_observed then
        return "native action lacks paired processing evidence; it will not be replayed"
    end
    return nil
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "combat", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "combat", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    local spawn = must(client.World:Spawn("CitadelBasicGunner.Noun"))
    check(scenario.Sleep(500))
    local before = must(client.World:Snapshot())
    local target = findTarget(before, spawn.object_id)
    if not before.is_available or target == nil or target.is_dead or target.hit_point <= 0 then
        must(client.Diagnostics:Capture("combat-unavailable"))
        return "published combat target has no live server observation"
    end
    must(client.Diagnostics:Capture("combat-before"))
    local attackError = attackTarget(client, target)
    if attackError ~= nil then
        local capture, captureError = client.Diagnostics:Capture("combat-input-failed")
        return attackError
            .. "; capture=" .. tostring(capture ~= nil and capture.manifest_path or captureError)
    end
    local previousHitPoint = target.hit_point
    local attackCount = 1
    local deadline = scenario.Time() + 15000
    while scenario.Time() < deadline do
        local worldSS = must(client.World:Snapshot())
        local observed = findTarget(worldSS, spawn.object_id)
        if worldSS.game_id ~= spawn.game_id or worldSS.peer_generation ~= spawn.peer_generation
            or worldSS.zone_generation ~= spawn.zone_generation or worldSS.hero == nil
            or worldSS.hero.index ~= 0 or worldSS.hero.is_defeated then
            must(client.Diagnostics:Capture("combat-owner-failed"))
            return "combat world identity changed or the single active hero was defeated"
        end
        if observed ~= nil and observed.is_dead and observed.hit_point <= 0 then
            must(client.Diagnostics:Capture("combat-complete"))
            return nil
        end
        -- Only actual intended-target damage permits another normal hold.
        -- A processed miss or unknown action outcome is never replayed.
        if observed ~= nil and observed.hit_point < previousHitPoint and attackCount < 5 then
            previousHitPoint = observed.hit_point
            attackCount = attackCount + 1
            attackError = attackTarget(client, observed)
            if attackError ~= nil then
                local capture, captureError = client.Diagnostics:Capture("combat-input-failed")
                return attackError .. "; capture=" .. tostring(capture ~= nil and capture.manifest_path or captureError)
            end
        end
        check(scenario.Sleep(200))
    end
    must(client.Diagnostics:Capture("combat-stuck"))
    return "no server-confirmed target death within fifteen seconds and at most five damage-confirmed holds"
end
