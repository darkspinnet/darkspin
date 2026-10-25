---One fresh focused mission: ordinary native target damage/death, then a real
---squad switch. Root may run this starter three times as independent sessions.
local must = scenario.Must
local check = scenario.Check

local function fail(client, message)
    local capture, captureError = client.Diagnostics:Capture("hero-switch-failed")
    return message .. "; diagnostic=" .. tostring(capture ~= nil and capture.manifest_path or captureError)
end

local function targetByID(worldSS, objectID)
    for _, npc in ipairs(worldSS.npcs) do
        if npc.object_id == objectID then
            return npc
        end
    end
    return nil
end

local function sameEpoch(initial, worldSS)
    return worldSS.is_available and worldSS.client_id == initial.client_id and worldSS.instance_id == initial.instance_id
        and worldSS.pid == initial.pid and worldSS.user_id == initial.user_id and worldSS.game_id == initial.game_id
        and worldSS.peer_generation == initial.peer_generation and worldSS.transport_generation == initial.transport_generation
        and worldSS.zone_generation == initial.zone_generation and worldSS.hero ~= nil and not worldSS.hero.is_defeated
end

local function killTarget(client, initial, objectID)
    local deadline = scenario.Time() + 15000
    local previousHitPoint = nil
    local isActionPending = false
    local holdCount = 0
    while scenario.Time() < deadline do
        local worldSS, worldError = client.World:Snapshot()
        if worldError ~= nil or worldSS == nil then
            return fail(client, "combat observation failed: " .. tostring(worldError))
        end
        if not sameEpoch(initial, worldSS) or worldSS.hero.index ~= 0 then
            return fail(client, "combat changed account/game/epochs/hero or defeated the initial hero")
        end
        local target = targetByID(worldSS, objectID)
        if target == nil then
            return fail(client, "correlated spawned target is absent from the bounded world snapshot")
        end
        if target.is_dead and target.hit_point <= 0 then
            return nil
        end
        if not isActionPending or target.hit_point < previousHitPoint then
            if holdCount >= 5 then
                return fail(client, "target remains alive after five bounded damage-backed native holds")
            end
            holdCount = holdCount + 1
            previousHitPoint = target.hit_point
            local receipt, actionError = client.World:Hold(target.position, 1000)
            if actionError ~= nil then
                return fail(client, "native hold stopped without replay: " .. tostring(actionError))
            end
            local click = receipt ~= nil and receipt.gameplay_click or nil
            if receipt == nil or not receipt.is_processed or click == nil
                or not click.is_press_poll_observed or not click.is_release_poll_observed
                or click.retained_handle == nil or click.retained_handle == 0 then
                return fail(client, "hold omitted paired processing or a retained native target; no replay")
            end
            isActionPending = true
        end
        check(scenario.Sleep(200))
    end
    return fail(client, "no server-confirmed death within the bounded focused combat interval")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "switch", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "switch", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    local spawn = must(client.World:Spawn("CitadelBasicGunner.Noun"))
    local initial = must(client.World:Snapshot())
    if not initial.is_available or initial.hero == nil or initial.hero.index ~= 0
        or initial.user_id ~= profile.user_id or spawn.game_id ~= initial.game_id
        or spawn.peer_generation ~= initial.peer_generation or spawn.zone_generation ~= initial.zone_generation then
        return fail(client, "focused spawn and initial active hero do not match this admitted account")
    end
    must(client.Diagnostics:Capture("hero-before-kill"))
    local killError = killTarget(client, initial, spawn.object_id)
    if killError ~= nil then
        return killError
    end
    must(client.Diagnostics:Capture("hero-after-kill"))
    local selection, selectionError = client.World:SelectHero(1)
    if selectionError ~= nil then
        return fail(client, "native hero selection stopped without replay: " .. tostring(selectionError))
    end
    if selection == nil or selection.world == nil or not sameEpoch(initial, selection.world)
        or selection.world.hero.index ~= 1 or selection.is_already_selected
        or selection.initial_controlled_handle == nil or selection.controlled_handle == nil
        or selection.initial_controlled_handle == selection.controlled_handle then
        return fail(client, "hero selection omitted actual changed native handle and requested server squad index")
    end
    must(client.Diagnostics:Capture("hero-after-switch"))
    check(client:Terminate())
    local closed = must(client:Snapshot())
    if closed.is_running then
        return "explicitly terminated hero-switch client still reports running"
    end
    return nil
end
