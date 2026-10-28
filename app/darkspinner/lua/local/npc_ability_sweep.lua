---Build-103 cooperative NPC ability survey on one configured scenario server.
---Run a bounded catalog slice per session; inspect both client packet traces offline.
local must, check = scenario.Must, scenario.Check
local startAt, maximumCases, castTimeoutMS = 19, 64, 30000

local function pause(ms) check(scenario.Sleep(ms)) end
local function record(label, fields)
    local parts = {"npc_ability_sweep", label, "at=" .. math.floor(scenario.Time())}
    for key, item in pairs(fields or {}) do
        parts[#parts + 1] = key .. "=" .. tostring(item)
    end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end
local function launch(server, index)
    local id = index == 1 and "npc-victim" or "npc-observer"
    local noun = index == 1 and 1667741389 or 700180862
    local profile = must(server:CreateProfile({
        name = id, hero_nouns = {noun, 117394854, 576185321}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = id,
        launch_mode = index == 1 and "normal" or "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    local window = must(client.UI:InspectWindow()).window
    if not window or window.width ~= 1280 or window.height ~= 720 then
        error("co-op NPC fixture requires 1280x720 owned windows")
    end
    record("client", {id = client.ID, pid = client.PID, instance = client.InstanceID})
    return client
end
local function menu(client, x, y)
    local receipt, clickError = client.UI:Click(x, y)
    if not receipt and string.find(tostring(clickError), "native_rejected: owned window rejected", 1, true) then
        pause(600)
        receipt, clickError = client.UI:Click(x, y)
    end
    if not receipt or not receipt.is_processed then
        error("native party/menu click failed: " .. tostring(clickError))
    end
    pause(900)
end
local function enter(clients)
    local leader, follower = clients[1], clients[2]
    menu(leader, 105, 35)
    menu(leader, 368, 182)
    menu(leader, 930, 265)
    menu(follower, 604, 53)
    local deadline = scenario.Time() + 12000
    while scenario.Time() < deadline do
        local first = must(leader.Party:Snapshot()).party
        local second = must(follower.Party:Snapshot()).party
        if first.id ~= 0 and first.id == second.id and #first.members == 2 and #second.members == 2 then
            break
        end
        pause(150)
    end
    menu(leader, 759, 498)
    menu(leader, 1036, 103)
    for _, client in ipairs(clients) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(leader.Mission:Select(1))
    must(leader.Mission:Start())
    check(scenario.Parallel({
        first = function() must(leader.Mission:Continue()); return nil end,
        second = function() must(follower.Mission:Continue()); return nil end,
    }))
    check(scenario.Parallel({
        first = function() must(leader.Mission:Wait()); return nil end,
        second = function() must(follower.Mission:Wait()); return nil end,
    }))
end
local function living(client)
    local world = must(client.World:SnapshotLiving())
    if not world.is_available or not world.hero or world.hero.is_defeated
        or not world.campaign or #world.campaign.members ~= 2 then
        error("co-op actor or roster unavailable")
    end
    return world
end
local function frame(client)
    local receipt = must(client.World:SnapshotNative())
    local native = receipt.gameplay_state
    if not receipt.is_processed or not native or not native.is_available
        or not native.is_world_active or native.frame_sequence == 0 then
        error("owned native gameplay frame unavailable")
    end
    return native.frame_sequence
end
local function observedHitPoint(client, userID)
    local world = living(client)
    for _, member in ipairs(world.campaign.members) do
        if member.user_id == userID then
            for _, character in ipairs(member.characters) do
                if character.index == 0 then return character.hit_point end
            end
        end
    end
    error("co-op observer omitted victim hero resource")
end
local function restoreVictim(client)
    local receipt = must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    if not receipt.is_queued or receipt.recipient_count ~= 2 then
        error("high-HP hero fixture was not queued to both clients")
    end
    local deadline = scenario.Time() + 4000
    while scenario.Time() < deadline do
        local hero = living(client).hero
        if math.abs(hero.hit_point - 1000000) < 1
            and math.abs(hero.maximum_hit_point - 1000000) < 1 then return hero end
        pause(80)
    end
    error("high-HP hero fixture did not settle")
end
local function positionNPC(npc, entry, hero, before)
    if entry.family ~= "melee" and entry.family ~= "cone" then return true end
    local dx, dy = before.position.x - hero.position.x, before.position.y - hero.position.y
    local distance = math.sqrt(dx * dx + dy * dy)
    local range = entry.range or 0
    if range > 0 and distance < range * 0.8 then return true end
    if distance < 0.1 then return true end
    local desired = range > 0 and math.min(3, math.max(1.6, range * 0.6)) or 2
    local minimum = entry.minimum_range or 0
    if minimum > desired then desired = minimum + 0.3 end
    local destination = {x = hero.position.x + dx / distance * desired,
        y = hero.position.y + dy / distance * desired, z = before.position.z}
    local moved, moveError = npc:Move(destination)
    if not moved or not moved.is_accepted or not moved.is_completed then
        return false, tostring(moveError or "manual NPC position did not complete")
    end
    return true
end
local function runCase(clients, entry, ordinal)
    local victim, observer = clients[1], clients[2]
    local hero = restoreVictim(victim)
    local npc, spawnError = victim.World:SpawnControlledNPC(entry.noun)
    if not npc then
        record("case_end", {ordinal = ordinal, noun = entry.noun,
            status = "spawn_failed", failure = tostring(spawnError)})
        return false
    end
    local before = must(npc:Snapshot())
    if not before.is_available or before.is_ai_enabled or before.is_dead
        or #before.supported_ability_names == 0 then
        error("controlled spawn omitted a live AI-off ability: " .. entry.noun)
    end
    local positioned, positionError = positionNPC(npc, entry, hero, before)
    if not positioned then
        record("case_end", {ordinal = ordinal, noun = entry.noun,
            status = "position_failed", failure = positionError})
        must(npc:Retire())
        return false
    end
    local peer = living(observer)
    if peer.game_id ~= living(victim).game_id then
        error("co-op observer left the NPC world")
    end
    local ability = before.supported_ability_names[1]
    local victimFrame, observerFrame = frame(victim), frame(observer)
    local remoteBefore = observedHitPoint(observer, living(victim).user_id)
    record("case_start", {ordinal = ordinal, noun = entry.noun,
        ability = ability, object = before.identity.object_id,
        victim_pid = victim.PID, observer_pid = observer.PID,
        victim_frame = victimFrame, observer_frame = observerFrame,
        observer_hp_before = remoteBefore, family = entry.family, rank = entry.rank,
        range = entry.range, minimum_range = entry.minimum_range})
    local cast, castError = npc:Cast(ability, hero.object_id, castTimeoutMS)
    if cast and cast.is_accepted and cast.is_settled then pause(2000) end
    local after = living(victim).hero
    local remote = living(observer)
    local observedHP = observedHitPoint(observer, living(victim).user_id)
    local settled = castError == nil and cast and cast.is_accepted and cast.is_settled
        and cast.is_response_committed
    local status = settled and "admitted" or after.hit_point < hero.hit_point
        and "effect_unsettled" or "cast_failed"
    record("case_end", {ordinal = ordinal, noun = entry.noun, ability = ability,
        status = status,
        failure = castError and tostring(castError) or "",
        victim_hp_before = hero.hit_point, victim_hp_after = after.hit_point,
        observer_hp_after = observedHP, observer_pid = remote.pid, victim_frame = frame(victim),
        observer_frame = frame(observer), is_packet_delivery_verified = false})
    local retired = must(npc:Retire())
    if not retired.is_accepted or not retired.is_settled then
        error("controlled NPC retirement did not settle: " .. entry.noun)
    end
    return settled
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local clients = {launch(server, 1), launch(server, 2)}
    enter(clients)
    must(clients[1].World:Ready())
    must(clients[2].World:Ready())
    local catalog = must(clients[1].World:NPCCatalog())
    record("catalog", {game = catalog.game_id, zone = catalog.zone_generation,
        level = catalog.level, difficulty = catalog.difficulty,
        entries = #catalog.entries, start = startAt, maximum = maximumCases})
    local eligible, attempted, admitted = 0, 0, 0
    for _, entry in ipairs(catalog.entries) do
        record("catalog_entry", {noun = entry.noun, ability = entry.ability_name,
            family = entry.family, rank = entry.rank, range = entry.range,
            minimum_range = entry.minimum_range, target_kind = entry.target_kind,
            is_action_recovered = entry.is_action_recovered,
            is_control_supported = entry.is_control_supported,
            is_final_boss = entry.is_final_boss})
        if entry.is_control_supported then
            eligible = eligible + 1
            if eligible >= startAt and attempted < maximumCases then
                attempted = attempted + 1
                if runCase(clients, entry, eligible) then admitted = admitted + 1 end
            end
        else
            record("unsupported", {noun = entry.noun, ability = entry.ability_name,
                family = entry.family, is_final_boss = entry.is_final_boss})
        end
    end
    record("suite_end", {eligible = eligible, attempted = attempted, admitted = admitted,
        untested = math.max(0, eligible - startAt - attempted + 1)})
    for _, client in ipairs(clients) do
        local capture = must(client.Diagnostics:Capture("npc-ability-sweep-final"))
        record("capture", {client = client.ID, pid = client.PID,
            manifest = capture.manifest_path, is_complete = capture.is_complete})
    end
    return nil
end
