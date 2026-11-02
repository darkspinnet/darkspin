---Build-103 cooperative NPC ability survey on one configured scenario server.
---Run a bounded catalog slice per session; inspect both client packet traces offline.
local must, check = scenario.Must, scenario.Check
local startAt, maximumCases, castTimeoutMS = 1, 588, 30000
local pairCount = 2
local caseFilter = "all" -- "all", "captain", "haster", "rezzer", or "shadowminion"
local selectedOrdinals = nil
local selectedNouns = {}
local runSecondaries = false
local selectedSecondaryAbilities = nil
local runCopterPairs = false

local function pause(ms) check(scenario.Sleep(ms)) end
local function record(label, fields)
    local parts = {"npc_ability_sweep", label, "at=" .. math.floor(scenario.Time())}
    for key, item in pairs(fields or {}) do
        parts[#parts + 1] = key .. "=" .. tostring(item)
    end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end
local function launch(server, index, pairIndex)
    local id = (index == 1 and "npc-victim-" or "npc-observer-") .. pairIndex
    local noun = index == 1 and 1667741389 or 700180862
    local profile = must(server:CreateProfile({
        name = id, hero_nouns = {noun, 117394854, 576185321}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = id,
        launch_mode = index == 1 and pairIndex == 1 and "normal" or "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    local window = must(client.UI:InspectWindow()).window
    if not window or window.width ~= 1280 or window.height ~= 720 then
        error("co-op NPC fixture requires 1280x720 owned windows")
    end
    record("client", {id = client.ID, pid = client.PID,
        instance = client.InstanceID, pair = pairIndex})
    return client
end
local function enter(clients, pairIndex)
    local leader, follower = clients[1], clients[2]
    must(leader.Party:Create("npc-sweep-" .. pairIndex))
    must(leader.Party:Invite(follower))
    must(follower.Party:Join(leader))
    local deadline = scenario.Time() + 12000
    while scenario.Time() < deadline do
        local first = must(leader.Party:Snapshot()).party
        local second = must(follower.Party:Snapshot()).party
        if first.id ~= 0 and first.id == second.id and #first.members == 2 and #second.members == 2 then
            break
        end
        pause(150)
    end
    for _, client in ipairs(clients) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(leader.Mission:Select(1))
    must(leader.Mission:Start())
    must(follower.Mission:WaitDeployment())
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
local function restoreHero(client)
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
local function resetHeroPosition(client, anchor)
    if not anchor then return end
    local current = living(client).hero.position
    local dx, dy = current.x - anchor.x, current.y - anchor.y
    if dx * dx + dy * dy < 1 then return end
    local placed = must(client.World:PlaceHero(anchor))
    if not placed.is_queued or placed.recipient_count ~= 2 then
        error("NPC sweep hero reset did not reach both clients")
    end
    pause(250)
end
local function positionNPC(client, npc, entry, hero, before, ability)
    local defaultRange = {
        CitadelSpecialTwo_HomingStun = 11.5,
        CryosBasicCharge = 11.5,
        FastSwipe = 2.2,
        NocturnaSpecialHomer = 8,
        NocturnaSpecialHomerMelee = 2.2,
        NomadBioSpecialTwoJumpAttack = 8,
        Puller = 11.5,
        Resurrect = 11.5,
        GhostlyBolt = 11.5,
        ScaldronBasicSinkhole_Projectile = 8,
        ScaldronBasicSinkhole_Sinkhole = 3,
        SleepMushroom = 11.5,
        StealthAttack = 8,
        VerdanthBasicPicky = 14,
        ZelemBasicHybridProjectile = 8,
        ZelemBasicHybridMelee = 1.5,
    }
    local selectedDistance = defaultRange[ability]
    if entry.family ~= "melee" and entry.family ~= "cone"
        and not (entry.family == "leap" and (entry.minimum_range or 0) > 0)
        and not selectedDistance then return true end
    local dx, dy = before.position.x - hero.position.x, before.position.y - hero.position.y
    local distance = math.sqrt(dx * dx + dy * dy)
    local range = entry.range or 0
    local minimum = entry.minimum_range or 0
    if not selectedDistance and range > 0 and distance < range * 0.8
        and distance > minimum + 0.5 then return true end
    if distance < 0.1 then return true end
    local desired = range > 0 and math.min(3, math.max(1.6, range * 0.6)) or 2
    if minimum > desired then desired = minimum + 3 end
    if selectedDistance then desired = selectedDistance end
    local unitX, unitY = dx / distance, dy / distance
    local directions = {{unitX, unitY}, {unitY, -unitX}, {-unitY, unitX}, {-unitX, -unitY}}
    local lastError = "no mesh-valid NPC position found"
    for _, direction in ipairs(directions) do
        local destination = {x = hero.position.x + direction[1] * desired,
            y = hero.position.y + direction[2] * desired, z = before.position.z}
        local moved, moveError = npc:Move(destination)
        if moved and moved.is_accepted and moved.is_completed then
            local after = must(npc:Snapshot())
            local actualX, actualY = after.position.x - hero.position.x, after.position.y - hero.position.y
            local actualDistance = math.sqrt(actualX * actualX + actualY * actualY)
            record("position", {noun = entry.noun, distance = actualDistance,
                minimum = minimum, range = range})
            if actualDistance >= minimum + 0.5 then
                if ability ~= "StealthAttack" then return true end
                local path = client.World:Path(after.position, hero.position)
                if path and path.is_available and path.is_complete and path.goal
                    and path.goal.distance <= 0.75 then return true end
                lastError = "stealth approach has no complete mesh corridor"
            else
                lastError = "manual NPC position stayed inside authored minimum range"
            end
        else
            lastError = tostring(moveError or "manual NPC position did not complete")
            if not string.find(lastError, "no nearby mesh projection", 1, true) then
                return false, lastError
            end
        end
    end
    if selectedDistance or entry.family == "melee" or entry.family == "cone"
        or entry.family == "leap" and minimum > 0 then
        for _, direction in ipairs(directions) do
            local goal = {x = before.position.x + direction[1] * desired,
                y = before.position.y + direction[2] * desired, z = hero.position.z}
            local path = client.World:Path(hero.position, goal)
            if path and path.is_available and path.is_complete and path.goal
                and path.goal.distance <= 0.75 then
                local placed, placeError = client.World:PlaceHero(path.goal.position)
                if not placed or not placed.is_queued or placed.recipient_count ~= 2 then
                    return false, tostring(placeError or "hero fixture placement did not queue")
                end
                pause(300)
                local current = living(client).hero
                local actualX = before.position.x - current.position.x
                local actualY = before.position.y - current.position.y
                record("position_hero", {noun = entry.noun,
                    distance = math.sqrt(actualX * actualX + actualY * actualY),
                    requested = desired})
                if math.sqrt(actualX * actualX + actualY * actualY) >= minimum + 0.5 then
                    return true
                end
            end
        end
    end
    return false, lastError
end
local function runCase(clients, entry, ordinal, requestedAbility)
    if entry.ability_name == "ScaldronBasicCopter_Passive" then
        record("case_end", {ordinal = ordinal, noun = entry.noun,
            ability = entry.ability_name, status = "needs_pair",
            failure = "passive beam requires a second copter"})
        return false
    end
    local victim, observer = clients[1], clients[2]
    restoreHero(victim)
    restoreHero(observer)
    resetHeroPosition(victim, clients.victimAnchor)
    resetHeroPosition(observer, clients.observerAnchor)
    local hero = living(victim).hero
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
    local ability = requestedAbility or before.supported_ability_names[1]
    local isSupported = false
    for _, name in ipairs(before.supported_ability_names) do
        if name == ability then isSupported = true end
    end
    if not isSupported then error("controlled actor omitted requested ability: " .. ability) end
    local helper = nil
    local helperObject = 0
    if ability == "ZelemBasicPackMeleeAttack" or ability == "CastZelemHasteBuff"
        or ability == "Resurrect" then
        local helperError
        local helperNoun = ability == "Resurrect" and "CitadelBasicGunner.Noun" or entry.noun
        helper, helperError = victim.World:SpawnControlledNPC(helperNoun)
        if not helper then
            must(npc:Retire())
            record("case_end", {ordinal = ordinal, noun = entry.noun,
                ability = ability, status = "helper_failed", failure = tostring(helperError)})
            return false
        end
        helperObject = must(helper:Snapshot()).identity.object_id
    end
    local positioned, positionError = positionNPC(victim, npc, entry, hero, before, ability)
    if not positioned then
        record("case_end", {ordinal = ordinal, noun = entry.noun,
            status = "position_failed", failure = positionError})
        must(npc:Retire())
        if helper then must(helper:Retire()) end
        return false
    end
    if ability == "Resurrect" then
        local sourceState = must(npc:Snapshot())
        local moved = must(helper:Move(sourceState.position))
        if not moved.is_accepted or not moved.is_completed then
            error("Rezzer corpse helper could not reach its source")
        end
        local defeat = must(helper:Defeat())
        local helperState = must(helper:Snapshot())
        if not defeat.is_accepted or not defeat.is_settled or not helperState.is_dead
            or helperState.hit_point > 0 then
            error("Rezzer corpse fixture was not published")
        end
        record("helper_defeated", {ordinal = ordinal, noun = entry.noun,
            helper_object = helperObject, source_x = sourceState.position.x,
            source_y = sourceState.position.y, helper_x = helperState.position.x,
            helper_y = helperState.position.y})
    end
    local primeAbility = nil
    if ability == "ScaldronBasicDoppler_Shot"
        and string.find(entry.noun, "scaldronbasicdopplerfake", 1, true) == nil then
        primeAbility = "ScaldronBasicDoppler_Clone"
    end
    if primeAbility then
        record("prime_start", {ordinal = ordinal, noun = entry.noun, ability = primeAbility})
        local prime, primeError = npc:Cast(primeAbility, hero.object_id, castTimeoutMS)
        if not prime or not prime.is_accepted or not prime.is_settled
            or not prime.is_response_committed then
            record("case_end", {ordinal = ordinal, noun = entry.noun,
                ability = ability, status = "prime_failed", failure = tostring(primeError)})
            must(npc:Retire())
            if helper then must(helper:Retire()) end
            return false
        end
        record("prime_end", {ordinal = ordinal, noun = entry.noun, ability = primeAbility})
        hero = restoreHero(victim)
        restoreHero(observer)
        local repositioned, repositionError = positionNPC(victim, npc, entry, hero,
            must(npc:Snapshot()), ability)
        if not repositioned then
            record("case_end", {ordinal = ordinal, noun = entry.noun,
                ability = ability, status = "position_failed", failure = tostring(repositionError)})
            must(npc:Retire())
            if helper then must(helper:Retire()) end
            return false
        end
    end
    local peer = living(observer)
    if peer.game_id ~= living(victim).game_id then
        error("co-op observer left the NPC world")
    end
    local victimFrame, observerFrame = frame(victim), frame(observer)
    local remoteBefore = observedHitPoint(observer, living(victim).user_id)
    record("case_start", {ordinal = ordinal, noun = entry.noun,
        ability = ability, object = before.identity.object_id,
        helper_object = helperObject,
        victim_pid = victim.PID, observer_pid = observer.PID,
        victim_frame = victimFrame, observer_frame = observerFrame,
        observer_hp_before = remoteBefore, family = entry.family, rank = entry.rank,
        range = entry.range, minimum_range = entry.minimum_range})
    local cast, castError = npc:Cast(ability, hero.object_id, castTimeoutMS)
    if cast and cast.is_accepted and cast.is_settled then pause(2000) end
    local isRevived = true
    if ability == "Resurrect" then
        isRevived = false
        local deadline = scenario.Time() + 4000
        while scenario.Time() < deadline do
            local helperState = must(helper:Snapshot())
            if helperState.is_available and not helperState.is_dead and helperState.hit_point > 0 then
                isRevived = true
                record("helper_revived", {ordinal = ordinal, noun = entry.noun,
                    helper_object = helperObject, hit_point = helperState.hit_point})
                break
            end
            pause(100)
        end
    end
    local after = living(victim).hero
    local remote = living(observer)
    local observedHP = observedHitPoint(observer, living(victim).user_id)
    local settled = castError == nil and cast and cast.is_accepted and cast.is_settled
        and cast.is_response_committed and isRevived
    local status = settled and "admitted" or after.hit_point < hero.hit_point
        and "effect_unsettled" or "cast_failed"
    record("case_end", {ordinal = ordinal, noun = entry.noun, ability = ability,
        status = not isRevived and "effect_missing" or status,
        failure = castError and tostring(castError) or "",
        victim_hp_before = hero.hit_point, victim_hp_after = after.hit_point,
        observer_hp_after = observedHP, observer_pid = remote.pid, victim_frame = frame(victim),
        observer_frame = frame(observer), is_packet_delivery_verified = false})
    local retired = must(npc:Retire())
    if not retired.is_accepted or not retired.is_settled then
        error("controlled NPC retirement did not settle: " .. entry.noun)
    end
    if helper then must(helper:Retire()) end
    return settled
end

local function runCopterPair(clients, entry, ordinal)
    local victim, observer = clients[1], clients[2]
    restoreHero(victim)
    restoreHero(observer)
    resetHeroPosition(victim, clients.victimAnchor)
    resetHeroPosition(observer, clients.observerAnchor)
    local hero = living(victim).hero
    local helper = must(victim.World:SpawnControlledNPC(entry.noun))
    local source = must(victim.World:SpawnControlledNPC(entry.noun))
    local helperState = must(helper:Snapshot())
    local sourceState = must(source:Snapshot())
    if helperState.identity.object_id >= sourceState.identity.object_id then
        error("copter helper must have the lower object identity")
    end
    local offsets = {{6, 0}, {-6, 0}, {0, 6}, {0, -6}}
    local goalPath = nil
    local positionError = "no mesh-valid paired copter position"
    for _, offset in ipairs(offsets) do
        local destination = {x = helperState.position.x + offset[1],
            y = helperState.position.y + offset[2], z = sourceState.position.z}
        local moved, moveError = source:Move(destination)
        if moved and moved.is_accepted and moved.is_completed then
            sourceState = must(source:Snapshot())
            local dx = sourceState.position.x - helperState.position.x
            local dy = sourceState.position.y - helperState.position.y
            local separation = math.sqrt(dx * dx + dy * dy)
            local midpoint = {x = (sourceState.position.x + helperState.position.x) / 2,
                y = (sourceState.position.y + helperState.position.y) / 2,
                z = hero.position.z}
            local path = victim.World:Path(hero.position, midpoint)
            if separation >= 4 and separation <= 14 and path and path.is_available
                and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                goalPath = path
                break
            end
        else
            positionError = tostring(moveError or "paired copter move did not complete")
            if not string.find(positionError, "no nearby mesh projection", 1, true) then break end
        end
    end
    if not goalPath then
        record("case_end", {ordinal = ordinal, noun = entry.noun,
            ability = entry.ability_name, status = "position_failed", failure = positionError})
        must(source:Retire())
        must(helper:Retire())
        return false
    end
    local placed = must(victim.World:PlaceHero(goalPath.goal.position))
    if not placed.is_queued or placed.recipient_count ~= 2 then
        error("paired copter hero placement did not reach both clients")
    end
    pause(300)
    hero = living(victim).hero
    local victimFrame, observerFrame = frame(victim), frame(observer)
    record("case_start", {ordinal = ordinal, noun = entry.noun,
        ability = entry.ability_name, object = sourceState.identity.object_id,
        helper_object = helperState.identity.object_id, rank = entry.rank,
        victim_pid = victim.PID, observer_pid = observer.PID,
        victim_frame = victimFrame, observer_frame = observerFrame,
        observer_hp_before = observedHitPoint(observer, living(victim).user_id)})
    local cast, castError = source:Cast(entry.ability_name, hero.object_id, castTimeoutMS)
    if cast and cast.is_accepted and cast.is_settled then pause(2000) end
    local after = living(victim).hero
    local observedHP = observedHitPoint(observer, living(victim).user_id)
    local settled = castError == nil and cast and cast.is_accepted and cast.is_settled
        and cast.is_response_committed
    record("case_end", {ordinal = ordinal, noun = entry.noun, ability = entry.ability_name,
        status = settled and "admitted" or "cast_failed",
        failure = castError and tostring(castError) or "",
        victim_hp_before = hero.hit_point, victim_hp_after = after.hit_point,
        observer_hp_after = observedHP, observer_pid = observer.PID,
        victim_frame = frame(victim), observer_frame = frame(observer),
        is_packet_delivery_verified = false})
    must(source:Retire())
    pause(1000)
    must(helper:Retire())
    return settled
end

return function()
    if selectedNouns and next(selectedNouns) == nil then
        record("suite_end", {attempted = 0, status = "no_pending_nouns"})
        return
    end
    check(scenario.RequireAPI(1))
    if pairCount < 1 or pairCount > 4 then error("NPC sweep supports one to four co-op pairs") end
    local server = must(scenario.StartServer({}))
    local pairs, launchBranches = {}, {}
    for index = 1, pairCount do
        local pair = {index = index, clients = {}}
        pairs[index] = pair
        launchBranches["launch_" .. index] = function()
            pair.clients[1] = launch(server, 1, index)
            pair.clients[2] = launch(server, 2, index)
            return nil
        end
    end
    check(scenario.Parallel(launchBranches))
    local gameIDs = {}
    for _, pair in ipairs(pairs) do
        enter(pair.clients, pair.index)
        must(pair.clients[1].World:Ready())
        must(pair.clients[2].World:Ready())
        local world = living(pair.clients[1])
        local peer = living(pair.clients[2])
        if gameIDs[world.game_id] or peer.game_id ~= world.game_id then
            error("NPC sweep pairs did not enter distinct co-op games")
        end
        gameIDs[world.game_id] = true
        pair.clients.victimAnchor = world.hero.position
        pair.clients.observerAnchor = peer.hero.position
        record("pair_ready", {pair = pair.index, game = world.game_id,
            victim_pid = pair.clients[1].PID, observer_pid = pair.clients[2].PID})
    end
    local catalog = must(pairs[1].clients[1].World:NPCCatalog())
    for index = 2, pairCount do
        local other = must(pairs[index].clients[1].World:NPCCatalog())
        if #other.entries ~= #catalog.entries or other.difficulty ~= catalog.difficulty then
            error("NPC sweep pairs have different installed catalogs")
        end
    end
    record("catalog", {game = catalog.game_id, zone = catalog.zone_generation,
        level = catalog.level, difficulty = catalog.difficulty,
        entries = #catalog.entries, start = startAt, maximum = maximumCases,
        pair_count = pairCount})
    local eligible, cases, secondaryCases, copterCases = 0, {}, {}, {}
    local secondaryOrdinal, copterOrdinal = 1000, 2000
    for _, entry in ipairs(catalog.entries) do
        record("catalog_entry", {noun = entry.noun, ability = entry.ability_name,
            supported = table.concat(entry.supported_ability_names or {}, ","),
            family = entry.family, rank = entry.rank, range = entry.range,
            minimum_range = entry.minimum_range, target_kind = entry.target_kind,
            is_action_recovered = entry.is_action_recovered,
            is_control_supported = entry.is_control_supported,
            is_final_boss = entry.is_final_boss})
        if entry.is_control_supported and (not selectedNouns or selectedNouns[string.lower(entry.noun)]) and (caseFilter == "all"
            or caseFilter == "captain" and string.find(string.lower(entry.noun), "_captain", 1, true)
            or caseFilter == "haster" and entry.ability_name == "CastZelemHasteBuff"
            or caseFilter == "rezzer" and entry.ability_name == "Resurrect"
            or caseFilter == "shadowminion" and string.find(entry.noun, "shadowbossminion", 1, true)) then
            eligible = eligible + 1
            if eligible >= startAt and #cases < maximumCases
                and (not selectedOrdinals or selectedOrdinals[eligible]) then
                cases[#cases + 1] = {ordinal = eligible, entry = entry}
            end
            if runSecondaries then
                for index = 2, #entry.supported_ability_names do
                    local ability = entry.supported_ability_names[index]
                    if not selectedSecondaryAbilities or selectedSecondaryAbilities[ability] then
                        secondaryOrdinal = secondaryOrdinal + 1
                        secondaryCases[#secondaryCases + 1] = {
                            ordinal = secondaryOrdinal, entry = entry, ability = ability}
                    end
                end
            end
            if runCopterPairs and entry.ability_name == "ScaldronBasicCopter_Passive" then
                copterOrdinal = copterOrdinal + 1
                copterCases[#copterCases + 1] = {ordinal = copterOrdinal, entry = entry}
            end
        else
            record("unsupported", {noun = entry.noun, ability = entry.ability_name,
                family = entry.family, is_final_boss = entry.is_final_boss})
        end
    end
    local castBranches = {}
    for _, pair in ipairs(pairs) do
        castBranches["cast_" .. pair.index] = function()
            pair.admitted, pair.secondaryAdmitted, pair.copterAdmitted = 0, 0, 0
            for index, case in ipairs(copterCases) do
                if (index - 1) % pairCount + 1 == pair.index then
                    if runCopterPair(pair.clients, case.entry, case.ordinal) then
                        pair.copterAdmitted = pair.copterAdmitted + 1
                    end
                end
            end
            for index, case in ipairs(cases) do
                if (index - 1) % pairCount + 1 == pair.index then
                    if runCase(pair.clients, case.entry, case.ordinal) then
                        pair.admitted = pair.admitted + 1
                    end
                end
            end
            for index, case in ipairs(secondaryCases) do
                if (index - 1) % pairCount + 1 == pair.index then
                    if runCase(pair.clients, case.entry, case.ordinal, case.ability) then
                        pair.secondaryAdmitted = pair.secondaryAdmitted + 1
                    end
                end
            end
            return nil
        end
    end
    local runError = scenario.Parallel(castBranches)
    local admitted, secondaryAdmitted, copterAdmitted = 0, 0, 0
    for _, pair in ipairs(pairs) do
        admitted = admitted + (pair.admitted or 0)
        secondaryAdmitted = secondaryAdmitted + (pair.secondaryAdmitted or 0)
        copterAdmitted = copterAdmitted + (pair.copterAdmitted or 0)
    end
    record("suite_end", {eligible = eligible, attempted = #cases, admitted = admitted,
        secondary_attempted = #secondaryCases, secondary_admitted = secondaryAdmitted,
        copter_attempted = #copterCases, copter_admitted = copterAdmitted,
        pair_count = pairCount, failure = tostring(runError or ""),
        untested = math.max(0, eligible - startAt - #cases + 1)})
    for _, pair in ipairs(pairs) do
        for _, client in ipairs(pair.clients) do
            local capture, captureError = client.Diagnostics:Capture("npc-ability-sweep-final")
            record("capture", {client = client.ID, pid = client.PID,
                manifest = capture and capture.manifest_path or "",
                is_complete = capture and capture.is_complete or false,
                failure = tostring(captureError or "")})
        end
    end
    for _, pair in ipairs(pairs) do
        for _, client in ipairs(pair.clients) do check(client:Terminate()) end
    end
    check(runError)
    return nil
end
