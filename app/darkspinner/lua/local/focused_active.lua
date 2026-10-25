---Narrow native SpecialTwo / server Ride the Lightning vertical slice.
---Normal cast admission is not damage or completion; server event phases and
---the independently observed target HP establish the actual outcome.
local must = scenario.Must
local check = scenario.Check

local function fail(client, message)
    local capture, captureError = client.Diagnostics:Capture("active-failed")
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

---@param before WorldSnapshot
---@param worldSS WorldSnapshot
local function isSameActor(before, worldSS)
    return worldSS.is_available and worldSS.instance_id == before.instance_id and worldSS.pid == before.pid
        and worldSS.user_id == before.user_id and worldSS.game_id == before.game_id
        and worldSS.peer_generation == before.peer_generation and worldSS.transport_generation == before.transport_generation
        and worldSS.zone_generation == before.zone_generation and worldSS.hero ~= nil
        and worldSS.hero.object_id == before.hero.object_id and worldSS.hero.index == before.hero.index
        and not worldSS.hero.is_defeated and worldSS.hero.hit_point > 0
end

---@param client ManagedClient
---@param target WorldNPC
---@return string|nil
local function holdTarget(client, target)
    local receipt, actionError = client.World:Hold(target.position, 1000)
    if actionError ~= nil then
        return "follow-up hold stopped without replay: " .. tostring(actionError)
    end
    local click = receipt ~= nil and receipt.gameplay_click or nil
    if receipt == nil or not receipt.is_processed or click == nil or not click.is_projected
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        return "follow-up hold omitted paired processing evidence; no replay"
    end
    return nil
end

---@param client ManagedClient
---@param baseline WorldSnapshot
---@return string|nil
local function followupCombat(client, baseline)
    local spawn, spawnError = client.World:Spawn("ZelemSpecialThree.Noun")
    if spawnError ~= nil or spawn == nil then
        return fail(client, "overlap target spawn: " .. tostring(spawnError))
    end
    local before, beforeError = client.World:Snapshot()
    if beforeError ~= nil or before == nil or not isSameActor(baseline, before) then
        return fail(client, "overlap before-state lost the pinned actor: " .. tostring(beforeError))
    end
    local target = targetByID(before, spawn.object_id)
    if target == nil or target.is_dead or target.hit_point <= 0 then
        return fail(client, "overlap target was not observed alive")
    end
    local availability, availabilityError = client.Ability:Snapshot()
    if availabilityError ~= nil or availability == nil or not availability.state.is_available
        or not availability.state.is_supported then
        return fail(client, "overlap ability observation unavailable: " .. tostring(availabilityError))
    end
    must(client.Diagnostics:Capture("active-followup-before"))
    local result, castError = client.Ability:Cast(target.position)
    if castError ~= nil then
        return fail(client, "overlap cast stopped without replay: " .. tostring(castError))
    end
    local cast = result ~= nil and result.observation ~= nil and result.observation.state.cast or nil
    if result == nil or not result.is_completed or cast == nil or not cast.is_accepted
        or not cast.is_arrived or not cast.is_arrival_processed or not cast.is_impact_committed
        or not cast.is_released or cast.is_failed or cast.target_object_id ~= target.object_id
        or cast.actor_object_id ~= before.hero.object_id or cast.creature_index ~= before.hero.index
        or result.observation.state.last_revision ~= availability.state.last_revision + 1 then
        return fail(client, "overlap cast omitted exact next intended-target impact/completion evidence")
    end
    local after, afterError = client.World:Snapshot()
    if afterError ~= nil or after == nil or not isSameActor(baseline, after) then
        return fail(client, "overlap after-state lost the pinned actor: " .. tostring(afterError))
    end
    local observed = targetByID(after, target.object_id)
    if observed == nil or observed.hit_point >= target.hit_point then
        return fail(client, "overlap cast did not independently reduce intended-target HP")
    end
    if observed.is_dead or observed.hit_point <= 0 then
        return fail(client, "overlap case unexercised: Ride the Lightning killed the target outright")
    end
    must(client.Diagnostics:Capture("active-followup-surviving"))
    local deadline = scenario.Time() + 20000
    local attackError = holdTarget(client, observed)
    if attackError ~= nil then
        return fail(client, attackError)
    end
    local previousHitPoint = observed.hit_point
    local holdCount = 1
    while scenario.Time() < deadline do
        local worldSS, worldError = client.World:Snapshot()
        if worldError ~= nil or worldSS == nil or not isSameActor(baseline, worldSS) then
            return fail(client, "follow-up state lost the pinned living actor: " .. tostring(worldError))
        end
        observed = targetByID(worldSS, target.object_id)
        if observed == nil then
            return fail(client, "follow-up target disappeared; death is unproven")
        end
        if observed.is_dead and observed.hit_point <= 0 then
            must(client.Diagnostics:Capture("active-followup-complete"))
            return nil
        end
        if observed.hit_point < previousHitPoint and holdCount < 8 then
            previousHitPoint = observed.hit_point
            holdCount = holdCount + 1
            attackError = holdTarget(client, observed)
            if attackError ~= nil then
                return fail(client, attackError)
            end
        end
        local sleepError = scenario.Sleep(200)
        if sleepError ~= nil then
            return fail(client, "follow-up observation wait: " .. tostring(sleepError))
        end
    end
    return fail(client, "surviving overlap target not defeated within twenty seconds and eight damage-confirmed holds")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "active", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "active", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    local spawn = must(client.World:Spawn("CitadelBasicGunner.Noun"))
    local before = must(client.World:Snapshot())
    local target = targetByID(before, spawn.object_id)
    if not before.is_available or before.hero == nil or before.hero.index ~= 0
        or target == nil or target.is_dead or target.hit_point <= 0 then
        return fail(client, "focused ability target/actor observation is unavailable")
    end
    local availability = must(client.Ability:Snapshot())
    if not availability.state.is_available or not availability.state.is_supported then
        return fail(client, "Ride the Lightning semantic observer does not support this actor")
    end
    must(client.Diagnostics:Capture("active-before"))
    local result, castError = client.Ability:Cast(target.position)
    if castError ~= nil then
        return fail(client, "active cast stopped without replay: " .. tostring(castError))
    end
    local cast = result ~= nil and result.observation ~= nil and result.observation.state.cast or nil
    if result == nil or not result.is_completed or cast == nil or not cast.is_accepted
        or not cast.is_arrived or not cast.is_arrival_processed or not cast.is_impact_committed or not cast.is_released or cast.is_failed
        or result.observation.state.last_revision ~= availability.state.last_revision + 1 then
        return fail(client, "cast omitted the exact next supported accepted/arrived/impact/release event")
    end
    local after = must(client.World:Snapshot())
    local observed = targetByID(after, spawn.object_id)
    if not isSameActor(before, after) or observed == nil or not observed.is_dead or observed.hit_point > 0 then
        return fail(client, "supported first cast completed without independently observed intended-target death")
    end
    must(client.Diagnostics:Capture("active-complete"))
    return followupCombat(client, before)
end
