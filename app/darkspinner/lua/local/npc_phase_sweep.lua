---Pending autonomous NPC phases on one fixed-port server.
local must, check = scenario.Must, scenario.Check
-- Add only unverified noun/action cohorts; completed captures are recorded in the audit notes.
local Contact = {}

local function distance(a, b)
    local dx, dy, dz = a.x - b.x, a.y - b.y, a.z - b.z
    return math.sqrt(dx * dx + dy * dy + dz * dz)
end

local function pending(emit, reason, isInputUnknown)
    local receipt = {is_confirmed = false, reason = reason, is_input_unknown = isInputUnknown == true}
    emit("fixture_setup_pending", receipt)
    return receipt
end

local function sample(client, center, emit)
    local world, worldError = client.World:SnapshotLiving()
    local nativeReceipt, nativeError = client.World:SnapshotNative()
    local native = nativeReceipt and nativeReceipt.gameplay_state
    if worldError or nativeError or not world or not world.hero or not native
        or not native.is_available or not native.is_world_active or not native.position
        or not native.frame_sequence or native.frame_sequence <= 0 then
        return nil, "living native/server position unavailable"
    end
    local server = world.hero.is_movement_available and world.hero.movement_position or world.hero.position
    if not server then return nil, "server hero position unavailable" end
    local receipt = {world = world, native = native, server = server,
        native_distance = distance(native.position, center), server_distance = distance(server, center)}
    emit("fixture_contact_sample", {client = client.ID, frame = native.frame_sequence,
        native_x = native.position.x, native_y = native.position.y, native_z = native.position.z,
        server_x = server.x, server_y = server.y, server_z = server.z,
        native_distance = receipt.native_distance, server_distance = receipt.server_distance})
    return receipt
end

-- Verifies actual positions over several native frames. PlaceHero's queued
-- receipt alone is deliberately never inspected as evidence of arrival.
function Contact.observe(client, center, limit, durationMs, emit)
    local deadline, observedCount, previousFrame = scenario.Time() + durationMs, 0, 0
    local hardDeadline = deadline + 1500
    repeat
        local receipt, reason = sample(client, center, emit)
        if not receipt then return pending(emit, reason) end
        if receipt.native_distance > limit or receipt.server_distance > limit then
            return pending(emit, "native/server contact is outside the authored radius")
        end
        if receipt.native.frame_sequence > previousFrame then observedCount = observedCount + 1 end
        previousFrame = receipt.native.frame_sequence
        if scenario.Time() >= deadline and observedCount >= 3 then
            emit("fixture_contact_confirmed", {client = client.ID, limit = limit,
                samples = observedCount, duration_ms = durationMs, frame = previousFrame})
            return {is_confirmed = true, limit = limit, samples = observedCount}
        end
        if scenario.Time() >= hardDeadline then return pending(emit, "native frame sequence did not advance") end
        local sleepError = scenario.Sleep(150)
        if sleepError then return pending(emit, "contact observation interrupted") end
    until false
end

function Contact.verifyPlacement(client, point, center, limit, emit)
    local receipt, placementError = client.World:PlaceHero(point)
    if placementError or not receipt then return pending(emit, "PlaceHero refused; no replay") end
    -- The command has no native arrival fence. Observe after multiple frames.
    local sleepError = scenario.Sleep(150)
    if sleepError then return pending(emit, "placement observation interrupted") end
    return Contact.observe(client, center, limit, 450, emit)
end

-- Complete mesh route + visible projection chooses a candidate. A consumed
-- click must additionally prove ground picking and native press/release polls.
-- Exactly one click is issued; a selected object or unknown edge is not replayed.
function Contact.enter(client, center, mode, emit)
    local initial, reason = sample(client, center, emit)
    if not initial then return pending(emit, reason) end
    local origin = initial.native.position
    local footprintPath, footprintError = client.World:Path(origin, origin)
    if footprintError or not footprintPath or not footprintPath.footprint_radius then
        return pending(emit, "hero navigation footprint unavailable")
    end
    local limit = type(mode) == "number" and mode
        or (mode == "vent" and 0.1 + footprintPath.footprint_radius or 3.5)
    if initial.native_distance <= limit and initial.server_distance <= limit then
        return Contact.observe(client, center, limit, 450, emit)
    end
    local goal
    local dx, dy = origin.x - center.x, origin.y - center.y
    local length = math.sqrt(dx * dx + dy * dy)
    local ux, uy = length > 0.001 and dx / length or 1, length > 0.001 and dy / length or 0
    for _, turn in ipairs({{1, 0}, {0.7071, 0.7071}, {0.7071, -0.7071}, {0, 1}, {0, -1}}) do
        local radius = math.max(0.05, limit - math.min(0.2, limit * 0.25))
        local candidate = {x = center.x + radius * (ux * turn[1] - uy * turn[2]),
            y = center.y + radius * (uy * turn[1] + ux * turn[2]), z = center.z}
        local path, pathError = client.World:Path(origin, candidate)
        local projected = path and path.goal and path.goal.position
        if not pathError and path and path.is_available and path.is_complete and not path.is_truncated
            and projected and distance(projected, center) < limit then
            local projectedReceipt, projectionError = client.World:Project(projected)
            local projection = projectedReceipt and projectedReceipt.projection
            if not projectionError and projection and projection.is_available and projection.is_on_screen then
                goal = projected
                break
            end
        end
    end
    if not goal then return pending(emit, "no complete visible ground route inside contact radius") end
    local receipt, clickError = client.World:Click(goal)
    local click = receipt and receipt.gameplay_click
    if clickError or not receipt or not receipt.is_processed or not click
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        return pending(emit, "native click outcome unconfirmed; no replay", true)
    end
    if not click.is_native_ground_query_success or click.retained_handle ~= 0 then
        return pending(emit, "native click selected an object instead of ground; callback setup skipped")
    end
    emit("fixture_contact_input", {client = client.ID, x = goal.x, y = goal.y, z = goal.z, limit = limit})
    local deadline = scenario.Time() + 6000
    repeat
        local sleepError = scenario.Sleep(150)
        if sleepError then return pending(emit, "native arrival observation interrupted") end
        local observed, observedError = sample(client, center, emit)
        if not observed then return pending(emit, observedError) end
        if observed.native.controlled_handle ~= initial.native.controlled_handle
            or observed.world.game_id ~= initial.world.game_id
            or observed.world.zone_generation ~= initial.world.zone_generation
            or observed.world.peer_generation ~= initial.world.peer_generation
            or observed.world.transport_generation ~= initial.world.transport_generation then
            return pending(emit, "hero or zone identity changed during contact setup")
        end
        if observed.native_distance <= limit and observed.server_distance <= limit then
            return Contact.observe(client, center, limit, 450, emit)
        end
    until scenario.Time() >= deadline
    return pending(emit, "processed ground click did not establish native/server arrival")
end


local Lethal = {}

local function pending(emit, reason, castCount, isInputUnknown)
    local result = {is_dead = false, is_confirmed = false, cast_count = castCount,
        is_input_unknown = isInputUnknown == true, reason = reason}
    emit("fixture_lethal_pending", result)
    return result
end

local function read(client)
    local world, worldError = client.World:SnapshotLiving()
    local probe, nativeError = client.World:SnapshotNative()
    local native = probe and probe.gameplay_state
    if worldError or nativeError or not world or not world.hero or not world.ability
        or not native or not native.is_available or not native.is_world_active or not native.is_rig_ready
        or not native.position or not native.frame_sequence or native.frame_sequence <= 0
        or probe.pid ~= client.PID or probe.instance_id ~= client.InstanceID then
        return nil, "owned living hero/native dispatch baseline unavailable"
    end
    return {world = world, native = native}
end

local function sameActor(observed, baseline)
    return observed and observed.hero and observed.game_id == baseline.game_id
        and observed.zone_generation == baseline.zone_generation
        and observed.peer_generation == baseline.peer_generation
        and observed.transport_generation == baseline.transport_generation
        and observed.hero.object_id == baseline.hero.object_id and observed.hero.noun == baseline.hero.noun
end

local function definition(noun)
    if noun == 709733043 then
        -- Production profile: hit170ms, release800ms, four one-second DOTs.
        return {asset = "FireTempestSupport", ability_id = 3139436766, slot = "special_one",
            window_ms = 5000, maximum_range = 35, aim_z = 0.3}
    end
    if noun == 185979955 then
        return {asset = "FieldMedicBasic", ability_id = 76539968, slot = "basic",
            window_ms = 1500, maximum_range = 20, aim_z = 0.3}
    end
    return nil
end

local function submit(client, aim, cast, emit)
    local projected, projectionError = client.World:Project(aim)
    local projection = projected and projected.projection
    if projectionError or not projection or not projection.is_available or not projection.is_on_screen then
        return false, "fixture aim is not visibly projected; no input submitted", false
    end
    if cast.slot == "special_one" then
        local button, buttonError = client.UI:Click(640, 655)
        if buttonError or not button or not button.is_processed then
            return false, "native SpecialOne selection unconfirmed; no replay", true
        end
        local sleepError = scenario.Sleep(300)
        if sleepError then return false, "ability selection interrupted; no target input submitted", false end
    end
    local receipt, inputError
    if cast.slot == "basic" then
        receipt, inputError = client.World:Hold(aim, 150)
    else
        receipt, inputError = client.World:Click(aim)
    end
    local click = receipt and receipt.gameplay_click
    if inputError or not receipt or not receipt.is_processed or not click
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        return false, "native target edge unconfirmed; no replay", true
    end
    emit("fixture_lethal_input", {asset = cast.asset, slot = cast.slot,
        retained_handle = click.retained_handle or "unknown", is_ground_query_success = click.is_native_ground_query_success,
        aim_x = aim.x, aim_y = aim.y, aim_z = aim.z})
    return true
end

-- job requires victim, observer and source (the owned ControlledNPC fixture).
-- All failures return an explicit pending receipt; do not replay that attempt.
-- Each next iteration is a NEW intentional cast after the prior bounded window.
function Lethal.run(job, emit)
    local source, observer, target = job.victim, job.observer, job.source
    local initial, initialError = read(source)
    if not initial then return pending(emit, initialError, 0) end
    local cast = definition(initial.world.hero.noun)
    if not cast then return pending(emit, "hero is neither Char Delta nor Meditron Beta", 0) end
    local targetBefore, targetError = target:Snapshot()
    if targetError or not targetBefore or not targetBefore.is_available then
        return pending(emit, "fixture baseline unavailable", 0)
    end
    if targetBefore.is_dead then
        emit("fixture_lethal_preexisting_death", {object = targetBefore.identity.object_id, hp = targetBefore.hit_point})
        return {is_dead = true, is_confirmed = false, cast_count = 0, reason = "fixture was dead before this helper"}
    end
    local objectID = targetBefore.identity.object_id
    local maxCasts = math.max(1, math.min(3, job.fixture_max_casts or 3))
    local previousCastRevision
    for castIndex = 1, maxCasts do
        local before, beforeError = read(source)
        local peerBefore, peerError = read(observer)
        local actor, actorError = target:Snapshot()
        if not before or not peerBefore or actorError or not actor or not actor.is_available
            or not sameActor(before.world, initial.world) or actor.identity.object_id ~= objectID then
            return pending(emit, beforeError or peerError or "cast actor/fixture identity changed", castIndex - 1)
        end
        if actor.is_dead then return {is_dead = true, is_confirmed = true, cast_count = castIndex - 1} end
        local previous = before.world.ability.last_dispatch and before.world.ability.last_dispatch.revision or 0
        if previousCastRevision and previous ~= previousCastRevision then
            return pending(emit, "another dispatch followed the previous intentional cast", castIndex - 1)
        end
        if castIndex > 1 then
            local reset, resetError = source.World:ResetCooldowns()
            if resetError or not reset or not reset.is_cooldown_reset or not reset.is_queued then
                return pending(emit, "completed prior cast cooldown reset unavailable", castIndex - 1)
            end
            emit("fixture_lethal_next_cast", {cast_index = castIndex, previous_dispatch_revision = previous,
                is_prior_authored_window_elapsed = true, is_producer_completion_observed = false})
        end
        local aim = {x = actor.position.x, y = actor.position.y, z = actor.position.z + cast.aim_z}
        local dx, dy, dz = aim.x - before.native.position.x, aim.y - before.native.position.y, aim.z - before.native.position.z
        if math.sqrt(dx * dx + dy * dy + dz * dz) > cast.maximum_range then
            return pending(emit, "fixture outside bounded direct-cast range; approach is not completion", castIndex - 1)
        end
        emit("fixture_lethal_before", {cast_index = castIndex, asset = cast.asset, ability_id = cast.ability_id,
            target = objectID, hp = actor.hit_point, source_pid = source.PID, observer_pid = observer.PID,
            source_frame = before.native.frame_sequence, observer_frame = peerBefore.native.frame_sequence,
            source_object = before.world.hero.object_id, previous_dispatch_revision = previous,
            source_x = before.native.position.x, source_y = before.native.position.y, source_z = before.native.position.z})
        local isSubmitted, inputReason, isInputUnknown = submit(source, aim, cast, emit)
        if not isSubmitted then return pending(emit, inputReason, castIndex - 1, isInputUnknown) end
        local dispatchDeadline = scenario.Time() + 1500
        local acceptedAt, dispatch, lastActor
        repeat
            local world, worldError = source.World:SnapshotLiving()
            local currentActor, currentError = target:Snapshot()
            if worldError or currentError or not world or not world.ability or not currentActor
                or not currentActor.is_available or currentActor.identity.object_id ~= objectID
                or not sameActor(world, initial.world) then
                return pending(emit, "cast outcome observation unavailable; no replay", castIndex, true)
            end
            lastActor = currentActor
            local current = world.ability.last_dispatch
            if current and current.revision > previous then
                if current.revision ~= previous + 1 or current.actor_object_id ~= initial.world.hero.object_id
                    or current.ability_asset ~= cast.asset or current.ability_id ~= cast.ability_id or current.slot ~= cast.slot then
                    return pending(emit, "fresh dispatch identity/revision disagrees with requested cast", castIndex, true)
                end
                if not dispatch then
                    dispatch, acceptedAt = current, scenario.Time()
                    emit("fixture_lethal_dispatch_accepted", {cast_index = castIndex, asset = current.ability_asset,
                        revision = current.revision, dispatch_target = current.target_object_id,
                        target = objectID, hp = currentActor.hit_point, is_impact_confirmed = false})
                end
            end
            if not dispatch and scenario.Time() >= dispatchDeadline then
                return pending(emit, "processed native input lacked exact fresh dispatch; no replay", castIndex, true)
            end
            if dispatch and scenario.Time() - acceptedAt >= cast.window_ms then break end
            local sleepError = scenario.Sleep(125)
            if sleepError then return pending(emit, "accepted cast observation interrupted; no replay", castIndex, true) end
        until false
        local after, afterError = read(source)
        local peerAfter, peerAfterError = read(observer)
        if not after or not peerAfter or not sameActor(after.world, initial.world)
            or not sameActor(peerAfter.world, peerBefore.world)
            or after.native.frame_sequence <= before.native.frame_sequence
            or peerAfter.native.frame_sequence <= peerBefore.native.frame_sequence then
            return pending(emit, afterError or peerAfterError or "caster/observer native progress unconfirmed", castIndex)
        end
        previousCastRevision = dispatch.revision
        emit("fixture_lethal_after", {cast_index = castIndex, asset = cast.asset, revision = dispatch.revision,
            target = objectID, hp_before = actor.hit_point, hp_after = lastActor.hit_point, is_dead = lastActor.is_dead,
            source_frame = after.native.frame_sequence, observer_frame = peerAfter.native.frame_sequence,
            elapsed_ms = scenario.Time() - acceptedAt, is_authored_window_elapsed = true,
            is_producer_completion_observed = false, is_packet_delivery_verified = false})
        if lastActor.is_dead then return {is_dead = true, is_confirmed = true, cast_count = castIndex} end
    end
    return pending(emit, "fixture remained alive after three separately accepted bounded casts", maxCasts)
end


local ObservedNPC = {}
local Observation = {}
Observation.__index = Observation

local function positive(number)
    return type(number) == "number" and number == number and number > 0 and number < math.huge
end

local function unavailable(reason)
    return {is_available = false, unavailable_reason = reason}, reason
end

local function matches(world, identity)
    return world and world.is_available and world.game_id == identity.game_id
        and world.peer_generation == identity.peer_generation
        and world.transport_generation == identity.transport_generation
        and world.zone_generation == identity.zone_generation
end

function Observation:Snapshot()
    local client, identity = self.client, self.identity
    if client.ID ~= self.client_id or client.PID ~= self.pid or client.InstanceID ~= self.instance_id then
        return unavailable("observed NPC owned process changed")
    end
    local world, worldError = client.World:SnapshotLiving()
    if worldError or not matches(world, identity) then
        return unavailable(worldError or "observed NPC world epoch unavailable or changed")
    end
    local actor
    for _, candidate in ipairs(world.npcs or {}) do
        if candidate.object_id == identity.object_id then
            if actor then return unavailable("observed NPC identity is ambiguous") end
            actor = candidate
        end
    end
    if not actor then
        return unavailable("observed NPC record missing; absence does not prove death")
    end
    if type(actor.noun) ~= "string" or string.lower(actor.noun) ~= self.noun
        or not actor.position or type(actor.hit_point) ~= "number" or actor.hit_point ~= actor.hit_point
        or type(actor.is_dead) ~= "boolean" then
        return unavailable("observed NPC noun or life record disagrees with spawn receipt")
    end
    return {is_available = true, identity = {
        user_id = identity.user_id, game_id = identity.game_id,
        peer_generation = identity.peer_generation, transport_generation = identity.transport_generation,
        zone_generation = identity.zone_generation, spawn_request_id = identity.spawn_request_id,
        object_id = identity.object_id},
        noun = actor.noun, position = {x = actor.position.x, y = actor.position.y, z = actor.position.z},
        hit_point = actor.hit_point, is_dead = actor.is_dead, is_published = actor.is_published,
        is_fixture = actor.is_fixture, source = "plain_world_npc_observation"}, nil
end

function ObservedNPC.new(client, receipt)
    if not client or type(receipt) ~= "table" or not receipt.is_admitted or not receipt.is_published
        or not positive(receipt.object_id) or not positive(receipt.game_id)
        or not positive(receipt.peer_generation) or not positive(receipt.transport_generation)
        or not positive(receipt.zone_generation) or type(receipt.noun) ~= "string"
        or type(receipt.request_id) ~= "string" or receipt.request_id == ""
        or receipt.client_id ~= client.ID or receipt.pid ~= client.PID or receipt.instance_id ~= client.InstanceID then
        return nil, "plain NPC observation requires this client's actual published World:Spawn receipt"
    end
    local observation = setmetatable({client = client, Receipt = receipt, client_id = client.ID,
        pid = client.PID, instance_id = client.InstanceID, noun = string.lower(receipt.noun),
        identity = {user_id = receipt.user_id, game_id = receipt.game_id,
            peer_generation = receipt.peer_generation, transport_generation = receipt.transport_generation,
            zone_generation = receipt.zone_generation, spawn_request_id = receipt.request_id,
            object_id = receipt.object_id}}, Observation)
    local snapshot, snapshotError = observation:Snapshot()
    if snapshotError then return nil, snapshotError end
    observation.InitialSnapshot = snapshot
    return observation, nil
end

-- Readiness is observation only. No status cleanse, cooldown mutation or input.
function ObservedNPC.waitCastReady(client, timeoutMs)
    local deadline = scenario.Time() + math.max(1, math.min(30000, timeoutMs or 15000))
    local initial
    repeat
        local world, worldError = client.World:SnapshotLiving()
        local probe, nativeError = client.World:SnapshotNative()
        local native = probe and probe.gameplay_state
        if worldError or nativeError or not world or not world.is_available or not world.hero
            or not native or not native.is_available or probe.pid ~= client.PID
            or probe.instance_id ~= client.InstanceID then
            return nil, worldError or nativeError or "cast readiness observation unavailable"
        end
        if initial and (not matches(world, initial) or world.hero.object_id ~= initial.object_id) then
            return nil, "cast readiness hero or world changed"
        end
        initial = initial or {game_id = world.game_id, peer_generation = world.peer_generation,
            transport_generation = world.transport_generation, zone_generation = world.zone_generation,
            object_id = world.hero.object_id}
        if world.hero.hit_point > 0 and world.hero.is_stunned == false and world.hero.is_silenced == false
            and type(world.hero.input_lock_remaining_ms) == "number" and world.hero.input_lock_remaining_ms <= 0
            and native.is_world_active and native.is_rig_ready then
            return {is_ready = true, world = world, native = native}, nil
        end
        if scenario.Time() >= deadline then return nil, "native cast control remained unavailable before deadline" end
        local sleepError = scenario.Sleep(125)
        if sleepError then return nil, "cast readiness observation interrupted" end
    until false
end


local jobs = {
    {id = "lane-1", hero = 709733043, cases = {
        {noun = "dest_prefab_islands_instrument_scitech_7.noun", is_projectile_lane = true},
        {noun = "dest_prefab_islands_instrument_scitech_7_noshadow.noun", is_projectile_lane = true},
        {noun = "dest_islands_instrument_scitech_7.noun", is_projectile_lane = true},
    }},
    {id = "lane-2", hero = 709733043, cases = {
        {noun = "dest_islands_instrument_scitech_8.noun", is_projectile_lane = true},
        {noun = "dest_prefab_islands_instrument_scitech_8.noun", is_projectile_lane = true},
    }},
    {id = "lane-ranged-replay", hero = 709733043, cases = {
        {noun = "TutorialBasicRanged.Noun", is_observed = true, sample_count = 12},
    }},
    {id = "lane-scaldron3-replay", hero = 709733043, cases = {
        {noun = "ScaldronBoss_3.Noun", is_observed = true, sample_count = 60, owner_attack_at = 20},
    }},
}
local function log(job, label, fields)
    local parts = {"npc_phase_sweep", label, "case=" .. job.id}
    for name, field in pairs(fields) do
        parts[#parts + 1] = name .. "=" .. tostring(field)
    end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end
local function launch(server, job, role)
    local noun = role == "victim" and job.hero or 185979955
    local nouns = {noun}
    for _, candidate in ipairs({117394854, 576185321, 1667741389}) do
        if candidate ~= noun and #nouns < 3 then nouns[#nouns + 1] = candidate end
    end
    local profile = must(server:CreateProfile({name = "phase-" .. job.id .. "-" .. role,
        hero_nouns = nouns, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "phase-" .. job.id .. "-" .. role,
        launch_mode = "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    log(job, "client", {role = role, pid = client.PID, user = profile.user_id})
    return client, profile
end
local function launchPair(server, job)
    job.victim, job.victimProfile = launch(server, job, "victim")
    job.observer, job.observerProfile = launch(server, job, "observer")
end
local function prepareParty(job)
    must(job.victim.Party:Create("phase-" .. job.id))
    must(job.victim.Party:Invite(job.observer))
    must(job.observer.Party:Join(job.victim))
end
local function prepare(job)
    for _, client in ipairs({job.victim, job.observer}) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(job.victim.Mission:Select(1))
    must(job.victim.Mission:Start())
    must(job.observer.Mission:WaitDeployment())
end
local function prepareWorld(job)
    must(job.victim.Mission:Wait())
    must(job.observer.Mission:Wait())
    for _, client in ipairs({job.victim, job.observer}) do
        must(client.World:Ready())
        must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    end
end
local function living(job, client)
    local deadline = scenario.Time() + 3000
    repeat
        local world = must(client.World:Snapshot())
        if world.is_available then
            if world.is_zone_terminal or not world.hero or world.hero.is_defeated or world.hero.hit_point <= 0 then
                error("available world lost living hero: " .. client.ID)
            end
            return world
        end
        log(job, "unavailable", {client = client.ID, reason = world.unavailable_reason})
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error("world remained unavailable: " .. client.ID)
end
local function hitOwner(job, objectID, attacker)
    local client = attacker or job.observer
    local world = living(job, client)
    for _, npc in ipairs(world.npcs) do
        if npc.object_id == objectID then
            if npc.is_dead then error("healing owner died before native hit") end
            local aim = {x = npc.position.x, y = npc.position.y, z = npc.position.z + 1.5}
            local projection
            local projectionDeadline = scenario.Time() + 30000
            repeat
                local receipt, projectionError = client.World:Project(aim)
                projection = receipt and receipt.projection
                if not projectionError and projection and projection.is_available and projection.is_on_screen then break end
                if scenario.Time() >= projectionDeadline then
                    log(job, "owner_native_input_pending", {client = client.ID, object = objectID, is_input_submitted = false})
                    return
                end
                check(scenario.Sleep(250))
            until false
            local receipt = must(client.World:Hold(aim, 150))
            local click = receipt.gameplay_click
            if not receipt.is_processed or not click or not click.is_press_poll_observed
                or not click.is_release_poll_observed then
                error("owner native hit input lacked paired polls; no replay")
            end
            log(job, "native_hit_input", {client = client.ID, object = objectID, before_hp = npc.hit_point})
            return
        end
    end
    error("healing owner unavailable before native hit")
end
local function castPetShield(job)
    local native = must(job.victim.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready or not native.position then
        error("reactive summon owner native rig unavailable")
    end
    local button = must(job.victim.UI:Click(575, 655))
    if not button.is_processed then error("reactive summon hotbar edge unprocessed") end
    check(scenario.Sleep(300))
    local aim = {x = native.position.x, y = native.position.y, z = native.position.z + 1.5}
    local click = must(job.victim.World:Click(aim))
    if not click.is_processed then error("reactive summon native target edge unprocessed") end
    local deadline = scenario.Time() + 3000
    repeat
        local world = living(job, job.victim)
        local dispatch = world.ability and world.ability.last_dispatch
        if dispatch and dispatch.ability_asset == "PlasmaSentinelActive" then
            log(job, "pet_shield", {ability = dispatch.ability_asset, revision = dispatch.revision})
            return
        end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error("reactive summon native cast lacked exact dispatch; no replay")
end
local function stageSource(job)
    local source = must(job.victim.World:SpawnControlledNPC(job.noun))
    job.source = source
    local actor = must(source:Snapshot())
    local world = living(job, job.victim)
    for _, direction in ipairs({{1,0}, {-1,0}, {0,1}, {0,-1},
        {0.7071,0.7071}, {-0.7071,0.7071}, {0.7071,-0.7071}, {-0.7071,-0.7071}}) do
        local requested = {x = world.hero.position.x + direction[1] * job.distance,
            y = world.hero.position.y + direction[2] * job.distance, z = world.hero.position.z}
        local dx, dy = requested.x - actor.position.x, requested.y - actor.position.y
        if dx * dx + dy * dy <= 38 * 38 then
            local path = must(job.victim.World:Path(actor.position, requested))
            if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                if dx * dx + dy * dy > 19 * 19 then
                    local midpoint = {x = (actor.position.x + path.goal.position.x) / 2,
                        y = (actor.position.y + path.goal.position.y) / 2,
                        z = (actor.position.z + path.goal.position.z) / 2}
                    local midpointPath = must(job.victim.World:Path(actor.position, midpoint))
                    if not midpointPath.is_available or not midpointPath.is_complete or not midpointPath.goal then
                        error("staged source midpoint path unavailable")
                    end
                    local step = must(source:Move(midpointPath.goal.position))
                    if not step.is_completed then error("staged source midpoint movement incomplete") end
                end
                local moved = must(source:Move(path.goal.position))
                if not moved.is_completed then error("staged source did not complete movement") end
                actor = must(source:Snapshot())
                world = living(job, job.victim)
                dx, dy = actor.position.x - world.hero.position.x, actor.position.y - world.hero.position.y
                local actualDistance = math.sqrt(dx * dx + dy * dy)
                if math.abs(actualDistance - job.distance) > 1 then
                    error("staged source did not reach its requested selector distance")
                end
                job.sourceOrigin = actor.position
                must(source:SetAI(true))
                log(job, "staged_source", {object = source.ObjectID, distance = math.sqrt(dx * dx + dy * dy)})
                return source.InitialSnapshot
            end
        end
    end
    error("staged source lacks a complete bounded mesh path")
end
local function summonCompanion(job)
    local source = must(job.victim.World:SpawnControlledNPC(job.noun))
    local window = must(job.victim.UI:InspectWindow()).window
    if not window or window.width ~= 1280 or window.height ~= 720 then
        error("native summon requires the established 1280x720 hotbar layout")
    end
    local native = must(job.victim.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready or not native.position then
        error("native summon hero rig is unavailable")
    end
    local aim = {x = native.position.x, y = native.position.y, z = native.position.z + 1.5}
    local projection = must(job.victim.World:Project(aim)).projection
    if not projection or not projection.is_on_screen then error("native summon aim is off screen") end
    local button = must(job.victim.UI:Click(640, 655))
    if not button.is_processed then error("summon hotbar input unprocessed; no replay") end
    check(scenario.Sleep(300))
    local cast = must(job.victim.World:Click(aim))
    if not cast.is_processed then error("summon world input unprocessed; no replay") end
    log(job, "native_summon_input", {hero = job.hero, ability = "SummonBeast"})
    check(scenario.Sleep(3000))
    must(source:SetAI(true))
    return source.InitialSnapshot
end
local function moveOwner(job)
    local world = living(job, job.victim)
    for _, offset in ipairs({{15,0}, {-15,0}, {0,15}, {0,-15}}) do
        local goal = {x = world.hero.position.x + offset[1],
            y = world.hero.position.y + offset[2], z = world.hero.position.z}
        local path = must(job.victim.World:Path(world.hero.position, goal))
        if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
            local projection = must(job.victim.World:Project(path.goal.position)).projection
            if projection and projection.is_on_screen then
                local click = must(job.victim.World:Click(path.goal.position))
                if not click.is_processed then error("owner movement input unprocessed; no replay") end
                log(job, "owner_move_input", {x = path.goal.position.x, y = path.goal.position.y})
                return
            end
        end
    end
    error("owner movement lacks a visible complete mesh path")
end
local function finish(job, receipt)
    if job.source then
        if not job.is_source_ai_disabled then must(job.source:SetAI(false)) end
        local retired = must(job.source:Retire())
        if not retired.is_accepted or not retired.is_settled then
            error("NPC retirement did not settle: " .. job.noun)
        end
        job.source = nil
    end
    if job.ally then must(job.ally:Retire()); job.ally = nil end
    check(scenario.Sleep(250))
    for _, client in ipairs({job.victim, job.observer}) do
        local world = living(job, client)
        local native = must(client.World:SnapshotNative()).gameplay_state
        if not native or not native.is_available or not native.is_world_active then
            error("native gameplay unavailable after retirement: " .. client.ID)
        end
        for _, member in ipairs(world.campaign.members) do
            for _, character in ipairs(member.characters) do
                if character.index == 0 then
                    log(job, "final_hero", {client = client.ID, user = member.user_id,
                        hp = character.hit_point, frame = native.frame_sequence, noun = job.noun})
                end
            end
        end
    end
    log(job, "capture_end", {object = receipt and receipt.object_id, noun = job.noun, time = scenario.Time()})
end
local function fixtureSample(job, label)
    local actor = must(job.source:Snapshot())
    log(job, label, {noun = job.noun, object = actor.identity.object_id,
        hp = actor.hit_point, is_dead = actor.is_dead, is_fixture = actor.is_fixture,
        is_retirement_ready = actor.is_retirement_ready, time = scenario.Time()})
    for _, client in ipairs({job.victim, job.observer}) do living(job, client) end
    if job.ally then
        local ally = must(job.ally:Snapshot())
        log(job, "fixture_helper", {noun = job.noun, object = ally.identity.object_id,
            hp = ally.hit_point, is_dead = ally.is_dead})
    end
    return actor
end
local ProjectileLane = {}
ProjectileLane.__index = ProjectileLane

function ProjectileLane:plan(center, helper)
    local caster = living(self.job, self.job.observer)
    local owner = living(self.job, self.job.victim)
    for _, offset in ipairs({{4,7}, {-4,7}, {5,7}, {-5,7}, {4,-7}, {-4,-7}, {6,7}, {6,-7}, {0,8}}) do
        local targetPoint = {x = center.x + offset[1], y = center.y + offset[2], z = center.z}
        local casterPoint = {x = center.x - (offset[1] == 0 and 8 or offset[1]),
            y = center.y + offset[2], z = center.z}
        local targetPath, targetError = self.job.victim.World:Path(helper.position, targetPoint)
        local casterPath, casterError = self.job.observer.World:Path(caster.hero.position, casterPoint)
        if not targetError and not casterError and matches(targetPath, owner) and matches(casterPath, caster)
            and targetPath.is_complete and casterPath.is_complete and not targetPath.is_truncated
            and not casterPath.is_truncated and targetPath.goal and casterPath.goal
            and distance(targetPath.goal.position, targetPoint) <= 0.8
            and distance(casterPath.goal.position, casterPoint) <= 0.8 then
            targetPoint, casterPoint = targetPath.goal.position, casterPath.goal.position
            local lanePath, laneError = self.job.observer.World:Path(casterPoint, targetPoint)
            local length = 0
            if lanePath and lanePath.points then
                for index = 2, #lanePath.points do
                    length = length + distance(lanePath.points[index - 1], lanePath.points[index])
                end
            end
            local direct = distance(casterPoint, targetPoint)
            local targetY, casterY = targetPoint.y - center.y, casterPoint.y - center.y
            if not laneError and matches(lanePath, caster) and lanePath.is_complete
                and not lanePath.is_truncated and lanePath.goal and lanePath.start
                and distance(lanePath.goal.position, targetPoint) <= 0.8
                and distance(lanePath.start.position, casterPoint) <= 0.8
                and length >= direct - 0.8 and length <= direct + 0.8 and direct >= 7 and direct <= 14
                and distance(targetPoint, center) <= 12 and distance(casterPoint, center) <= 12
                and targetY * casterY > 0 and math.min(math.abs(targetY), math.abs(casterY)) >= 6.5 then
                self.target_point, self.caster_point = targetPoint, casterPoint
                log(self.job, "projectile_lane_plan", {target_x = targetPoint.x, target_y = targetPoint.y,
                    caster_x = casterPoint.x, caster_y = casterPoint.y, path_length = length,
                    direct_length = direct, target_path_points = #targetPath.points,
                    caster_path_points = #casterPath.points})
                return true
            end
        end
    end
    return false, "no complete projected straight lane outside foundation was available"
end

function ProjectileLane:world()
    local world, reason = self.job.observer.World:SnapshotLiving()
    if reason or not world or not world.hero or not world.ability
        or not matches(world, self.epoch) or world.hero.object_id ~= self.actor_id
        or world.hero.noun ~= 185979955 or world.hero.hit_point <= 0 then
        return nil, reason or "projectile lane caster/world identity changed"
    end
    return world
end

function ProjectileLane:cast(index)
    local world, reason = self:world()
    if not world then return false, reason, false end
    local target, targetError = self.job.ally:Snapshot()
    local fixture, fixtureError = self.job.source:Snapshot()
    if targetError or fixtureError or not target or not target.is_available or target.is_dead
        or target.identity.object_id ~= self.target_id or not fixture or not fixture.is_available
        or fixture.is_dead or fixture.identity.object_id ~= self.receipt.object_id then
        return false, "projectile lane lost living target/fixture identity", false
    end
    if distance(target.position, self.target_point) > 0.8 then
        return false, "projectile lane target left verified placement", false
    end
    local reset, resetError = self.job.observer.World:ResetCooldowns()
    if resetError or not reset or not reset.is_queued or not reset.is_cooldown_reset then
        return false, "projectile lane cooldown reset unavailable", false
    end
    local previous = world.ability.last_dispatch and world.ability.last_dispatch.revision or 0
    local aim = {x = target.position.x, y = target.position.y, z = target.position.z + 1.5}
    local projection, projectionError = self.job.observer.World:Project(aim)
    if projectionError or not projection or not projection.projection or not projection.projection.is_available
        or not projection.projection.is_on_screen then
        return false, "projectile lane target not visibly projected", false
    end
    local input, inputError = self.job.observer.World:Hold(aim, 150)
    local click = input and input.gameplay_click
    if inputError or not input or not input.is_processed or not click
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        return false, "projectile lane native edge unknown; no replay", true
    end
    local deadline, acceptedAt, revision = scenario.Time() + 1500
    repeat
        local current, currentError = self:world()
        if not current then return false, currentError, true end
        local dispatch = current.ability.last_dispatch
        if dispatch and dispatch.revision > previous then
            if dispatch.revision ~= previous + 1 or dispatch.actor_object_id ~= self.actor_id
                or dispatch.ability_asset ~= "FieldMedicBasic" or dispatch.ability_id ~= 76539968
                or dispatch.slot ~= "basic" or dispatch.target_object_id ~= self.target_id then
                return false, "projectile lane fresh dispatch disagrees; no replay", true
            end
            if not revision then
                revision, acceptedAt = dispatch.revision, scenario.Time()
                log(self.job, "projectile_lane_dispatch", {noun = self.job.noun, cast_index = index,
                    revision = revision, asset = dispatch.ability_asset, ability_id = dispatch.ability_id,
                    slot = dispatch.slot, actor = self.actor_id, target = self.target_id,
                    fixture = self.receipt.object_id, caster_pid = self.job.observer.PID,
                    observer_pid = self.job.victim.PID, time = acceptedAt})
            end
        end
        if not revision and scenario.Time() >= deadline then
            return false, "projectile lane lacks fresh dispatch; no replay", true
        end
        if revision and scenario.Time() >= acceptedAt + 3000 then break end
        check(scenario.Sleep(100))
    until false
    local after, afterError = self.job.ally:Snapshot()
    if afterError or not after or not after.is_available or after.is_dead or after.identity.object_id ~= self.target_id then
        return false, "projectile lane flight outcome unavailable", true
    end
    for _, client in ipairs({self.job.victim, self.job.observer}) do
        local native = must(client.World:SnapshotNative()).gameplay_state
        if not native or not native.is_available or not native.is_world_active or native.frame_sequence <= 0 then
            return false, "projectile lane native playback unavailable", true
        end
        log(self.job, "projectile_lane_flight", {noun = self.job.noun, client = client.ID,
            cast_index = index, revision = revision, target = self.target_id, hp_before = target.hit_point,
            hp_after = after.hit_point, frame = native.frame_sequence,
            is_authored_window_elapsed = true, is_packet_delivery_verified = false})
    end
    self.cast_count = index
    return true
end

function ProjectileLane:finish(reason, isInputUnknown)
    if reason then
        log(self.job, "projectile_lane_pending", {noun = self.job.noun, reason = tostring(reason),
            cast_count = self.cast_count, is_input_unknown = isInputUnknown == true})
        if isInputUnknown then check(scenario.Sleep(3000)) end
    end
    if self.job.source then
        local deadline = scenario.Time() + 5000
        repeat
            local actor = must(self.job.source:Snapshot())
            if actor.is_retirement_ready then break end
            if scenario.Time() >= deadline then error("projectile lane retirement remains unavailable") end
            check(scenario.Sleep(100))
        until false
    end
    log(self.job, "projectile_lane_end", {noun = self.job.noun, cast_count = self.cast_count,
        is_confirmed = not reason and self.cast_count == 12})
    finish(self.job, self.receipt)
end

local function runProjectileLane(job)
    local lane = setmetatable({job = job, cast_count = 0}, ProjectileLane)
    local world = living(job, job.victim)
    local predicted = {x = world.hero.position.x + 5, y = world.hero.position.y, z = world.hero.position.z}
    job.ally = must(job.victim.World:SpawnControlledNPC("BabyMortar.Noun"))
    local helper = must(job.ally:Snapshot())
    local isPlanned, planError = lane:plan(predicted, helper)
    if not isPlanned then lane:finish(planError, false); return end
    local moved, moveError = job.ally:Move(lane.target_point)
    local afterMove, afterMoveError = job.ally:Snapshot()
    local command = moved or (afterMove and afterMove.last_command)
    log(job, "projectile_lane_move", {noun = job.noun, object = helper.identity.object_id,
        requested_x = lane.target_point.x, requested_y = lane.target_point.y, requested_z = lane.target_point.z,
        before_x = helper.position.x, before_y = helper.position.y,
        after_x = afterMove and afterMove.position and afterMove.position.x,
        after_y = afterMove and afterMove.position and afterMove.position.y,
        is_completed = command and command.is_completed, is_accepted = command and command.is_accepted,
        is_settled = command and command.is_settled, state = command and command.state,
        request_id = command and command.request_id, failure = command and command.failure,
        error = tostring(moveError or afterMoveError),
        projected_x = command and command.projected_destination and command.projected_destination.x,
        projected_y = command and command.projected_destination and command.projected_destination.y})
    if moveError or not moved or not moved.is_completed then
        lane:finish("projectile lane helper placement unavailable: " .. tostring(moveError or (moved and moved.failure) or (moved and moved.state)), false); return
    end
    local target = must(job.ally:Snapshot())
    if not target.is_available or target.is_dead or target.is_ai_enabled ~= false
        or distance(target.position, lane.target_point) > 0.8 then
        lane:finish("projectile lane helper did not arrive before fixture birth", false); return
    end
    lane.target_id = target.identity.object_id
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    job.is_source_ai_disabled = true
    lane.receipt = job.source.InitialSnapshot
    local fixture = fixtureSample(job, "projectile_lane_before")
    if fixture.is_dead or distance(fixture.position, predicted) > 0.8 then
        lane:finish("projectile lane actual fixture center differs from prediction", false); return
    end
    log(job, "spawn", {noun = job.noun, object = lane.receipt.object_id, game = lane.receipt.game_id,
        target = lane.target_id, victim_pid = job.victim.PID, observer_pid = job.observer.PID, time = scenario.Time()})
    local point = lane.caster_point
    if math.min(math.abs(target.position.y - fixture.position.y), math.abs(point.y - fixture.position.y)) < 6.5
        or distance(target.position, fixture.position) > 12.5 or distance(point, fixture.position) > 12.5 then
        lane:finish("projectile lane actual placement violates foundation/aura bounds", false); return
    end
    local placement = Contact.verifyPlacement(job.observer, point, point, 0.8,
        function(label, fields) log(job, label, fields) end)
    if not placement.is_confirmed then
        lane:finish("projectile lane observer native/server placement unconfirmed", false); return
    end
    local observer = living(job, job.observer)
    lane.epoch, lane.actor_id = observer, observer.hero.object_id
    log(job, "projectile_lane_geometry", {noun = job.noun, fixture = lane.receipt.object_id,
        target = lane.target_id, caster = lane.actor_id, center_x = fixture.position.x,
        center_y = fixture.position.y, target_x = target.position.x, target_y = target.position.y,
        caster_x = point.x, caster_y = point.y, maximum_casts = 12})
    for index = 1, 12 do
        local isConfirmed, reason, isInputUnknown = lane:cast(index)
        if not isConfirmed then lane:finish(reason, isInputUnknown); return end
    end
    local finalFixture = must(job.source:Snapshot())
    if finalFixture.is_dead then lane:finish("projectile lane fixture did not remain alive", false); return end
    lane:finish()
end
local function runFixture(job)
    -- Position a helper before admitting a living aura that may forbid movement.
    if job.fixture_helper then
        job.ally = must(job.victim.World:SpawnControlledNPC("BabyMortar.Noun"))
        local helper = must(job.ally:Snapshot())
        local placement = must(job.ally:Move({x = helper.position.x, y = helper.position.y + (job.fixture_helper_distance or 2.5), z = helper.position.z}))
        if not placement.is_completed then error("fixture helper placement did not complete") end
    end
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    job.is_source_ai_disabled = true
    local receipt = job.source.InitialSnapshot
    local actor = fixtureSample(job, "fixture_before")
    if not actor.is_fixture then error("fixture admission lost its role") end
    log(job, "spawn", {noun = job.noun, object = receipt.object_id, is_fixture = true,
        game = receipt.game_id, victim_pid = job.victim.PID, observer_pid = job.observer.PID,
        time = scenario.Time()})
    if job.fixture_helper then
        fixtureSample(job, "fixture_helper_ready")
    end
    if job.fixture_contact then
        local contact
        if job.fixture_contact == "mushroom" then
            local native = must(job.victim.World:SnapshotNative()).gameplay_state
            local dx, dy = native.position.x - actor.position.x, native.position.y - actor.position.y
            local length = math.sqrt(dx * dx + dy * dy)
            local ux, uy = length > 0.001 and dx / length or 1, length > 0.001 and dy / length or 0
            local point = {x = actor.position.x + ux * 2.5, y = actor.position.y + uy * 2.5, z = actor.position.z}
            contact = Contact.verifyPlacement(job.victim, point, actor.position, 3.5,
                function(label, fields) log(job, label, fields) end)
        else
            contact = Contact.enter(job.victim, actor.position, job.fixture_contact,
                function(label, fields) log(job, label, fields) end)
        end
        if contact.is_confirmed and job.fixture_contact == "mushroom" then
            Contact.observe(job.victim, actor.position, 3.5, 4200,
                function(label, fields) log(job, label, fields) end)
        end
    end
    if job.fixture_projectile and job.ally then
        local target = must(job.ally:Snapshot())
        if not target.is_dead then
            local shot = must(job.observer.World:Hold({x = target.position.x, y = target.position.y, z = target.position.z + 1.5}, 150))
            local click = shot.gameplay_click
            if not shot.is_processed or not click or not click.is_press_poll_observed or not click.is_release_poll_observed then
                error("fixture projectile native edge unconfirmed; no replay")
            end
            log(job, "fixture_projectile_input", {client = job.observer.ID, target = target.identity.object_id})
            check(scenario.Sleep(1200))
        end
    end
    if job.fixture_pre_ms > 0 then
        check(scenario.Sleep(job.fixture_pre_ms))
        fixtureSample(job, "fixture_passive")
    end
    if job.fixture_aura_exit then
        local world = living(job, job.victim)
        local requested = {x = actor.position.x - 20, y = actor.position.y, z = actor.position.z}
        local path = must(job.victim.World:Path(world.hero.position, requested))
        if not path.is_available or not path.is_complete or not path.goal then
            error("fixture aura exit lacks a complete path")
        end
        must(job.victim.World:PlaceHero(path.goal.position))
        check(scenario.Sleep(1000))
        fixtureSample(job, "fixture_aura_exit")
        must(job.victim.World:PlaceHero(world.hero.position))
    end
    local lethal = Lethal.run(job, function(label, fields) log(job, label, fields) end)
    actor = must(job.source:Snapshot())
    log(job, "fixture_cast_summary", {noun = job.noun, object = actor.identity.object_id,
        cast_count = lethal.cast_count, is_confirmed = lethal.is_confirmed, is_dead = actor.is_dead})
    fixtureSample(job, actor.is_dead and "fixture_lethal" or "fixture_not_defeated")
    check(scenario.Sleep(job.fixture_post_ms))
    fixtureSample(job, "fixture_outcome")
    local retirementDeadline = scenario.Time() + 15000
    repeat
        actor = must(job.source:Snapshot())
        if actor.is_retirement_ready then break end
        check(scenario.Sleep(250))
    until scenario.Time() >= retirementDeadline
    if not actor.is_retirement_ready then error("fixture normal callbacks did not settle before retirement") end
    finish(job, receipt)
end
local function run(job)
    if job.is_projectile_lane then runProjectileLane(job); return end
    if job.is_fixture then runFixture(job); return end
    if job.catalog_only then
        local catalog = must(job.victim.World:NPCCatalog())
        for _, entry in ipairs(catalog.entries) do
            log(job, "catalog_entry", {noun = entry.noun, ability = entry.ability_name,
                rank = entry.rank, family = entry.family, hp = entry.hit_point,
                is_recovered = entry.is_action_recovered,
                is_manual = entry.is_control_supported, is_staging = entry.is_staging_supported,
                is_boss = entry.is_final_boss,
                manual = table.concat(entry.supported_ability_names or {}, ","),
                autonomous = table.concat(entry.autonomous_ability_names or {}, ",")})
        end
        log(job, "catalog_end", {count = #catalog.entries})
        return
    end
    if job.helper then
        job.ally = must(job.victim.World:SpawnControlledNPC("BabyMortar.Noun"))
        if job.helper_distance then
            local helper = must(job.ally:Snapshot())
            local moved = must(job.ally:Move({x = helper.position.x, y = helper.position.y + job.helper_distance, z = helper.position.z}))
            if not moved.is_completed then error("mutation helper preposition did not complete") end
        end
        log(job, "helper", {object = must(job.ally:Snapshot()).identity.object_id})
    end
    local receipt
    local observed
    if job.is_observed then
        receipt = must(job.victim.World:Spawn(job.noun))
        observed = must(ObservedNPC.new(job.victim, receipt))
    elseif job.companion_slow then
        receipt = summonCompanion(job)
    elseif job.staged_source then
        receipt = stageSource(job)
    elseif job.cases then
        job.source = must(job.victim.World:SpawnControlledNPC(job.noun))
        receipt = job.source.InitialSnapshot
        if job.contact_source_at then
            job.sourceOrigin = must(job.source:Snapshot()).position
        end
        if job.pet_shield then castPetShield(job) end
        must(job.source:SetAI(true))
    else
        receipt = must(job.victim.World:Spawn(job.noun))
    end
    log(job, "spawn", {noun = job.noun or receipt.noun, object = receipt.object_id,
        game = receipt.game_id, is_ai_enabled = job.cases ~= nil or job.staged_source or receipt.is_ai_enabled,
        victim_pid = job.victim.PID, observer_pid = job.observer.PID,
        time = scenario.Time()})
    local goal = nil
    if job.distance and not job.staged_source then
        local world = living(job, job.victim)
        for _, npc in ipairs(world.npcs) do
            if npc.object_id == receipt.object_id then
                for _, direction in ipairs({{1,0}, {-1,0}, {0,1}, {0,-1},
                    {0.7071,0.7071}, {-0.7071,0.7071}, {0.7071,-0.7071}, {-0.7071,-0.7071}}) do
                    local requested = {x = npc.position.x + direction[1] * job.distance,
                        y = npc.position.y + direction[2] * job.distance, z = npc.position.z}
                    local path = must(job.victim.World:Path(npc.position, requested))
                    if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                        goal = path.goal.position
                        break
                    end
                end
                if not goal then error("pending action has no complete positioning path") end
                must(job.victim.World:PlaceHero(goal))
                must(job.observer.World:PlaceHero(goal))
            end
        end
        if not goal then error("pending action source actor unavailable") end
    end
    for sample = 1, job.sample_count or (job.heal_owner and 30 or 12) do
        check(scenario.Sleep((job.heal_owner or job.fast_samples) and 1000 or 3000))
        if job.contact_source_at == sample then
            local actor = must(job.source:Snapshot())
            local origin = job.sourceOrigin
            local contact = {x = (origin.x + actor.position.x) / 2,
                y = (origin.y + actor.position.y) / 2, z = actor.position.z}
            must(job.victim.World:PlaceHero(contact))
            log(job, "line_contact", {object = receipt.object_id,
                x = contact.x, y = contact.y, z = contact.z})
        end
        if job.stop_ai_at == sample then
            must(job.source:SetAI(false))
            job.is_source_ai_disabled = true
            log(job, "stop_ai", {object = receipt.object_id})
        end
        if job.owner_attack_at == sample then hitOwner(job, receipt.object_id, job.victim) end
        if job.companion_slow and sample == 2 then moveOwner(job) end
        if job.heal_owner and sample == 9 then hitOwner(job, receipt.object_id) end
        if goal then
            must(job.victim.World:PlaceHero(goal))
            must(job.observer.World:PlaceHero(goal))
        end
        for _, client in ipairs({job.victim, job.observer}) do
            local world = living(job, client)
            log(job, "sample", {client = client.ID, sample = sample,
                hp = world.hero.hit_point, npc_count = #world.npcs,
                hero_x = world.hero.position.x, hero_y = world.hero.position.y})
            if job.companion_slow then
                local native = must(client.World:SnapshotNative()).gameplay_state
                log(job, "native_owner", {client = client.ID, sample = sample,
                    frame = native and native.frame_sequence,
                    x = native and native.position and native.position.x,
                    y = native and native.position and native.position.y})
            end
            for _, npc in ipairs(world.npcs) do
                if not npc.is_fixture and string.sub(string.lower(npc.noun), 1, 5) ~= "dest_" then
                log(job, "actor", {client = client.ID, sample = sample, object = npc.object_id,
                    noun = npc.noun, hp = npc.hit_point, is_dead = npc.is_dead,
                    x = npc.position.x, y = npc.position.y, z = npc.position.z})
                end
            end
        end
    end
    if observed and not job.skip_kill then
        local readiness, readinessError = ObservedNPC.waitCastReady(job.victim, 15000)
        if readiness then
            job.source = observed
            local lethal = Lethal.run(job, function(label, fields) log(job, label, fields) end)
            local actor = must(observed:Snapshot())
            log(job, "observed_cleanup", {noun = job.noun, object = receipt.object_id,
                hp = actor.hit_point, is_dead = actor.is_dead, cast_count = lethal.cast_count})
            job.source = nil
        else
            log(job, "observed_cleanup_pending", {noun = job.noun, reason = tostring(readinessError)})
        end
    end
    finish(job, receipt)
end
local function runBatch(job)
    if not job.cases then run(job); return end
    job.fast_samples = true
    local victimAnchor = living(job, job.victim).hero.position
    local observerAnchor = living(job, job.observer).hero.position
    for _, case in ipairs(job.cases) do
        job.noun = case.noun
        job.is_observed = case.is_observed
        job.skip_kill = case.skip_kill
        job.distance = case.distance
        job.staged_source = case.distance ~= nil
        job.sample_count = case.sample_count or 18
        job.stop_ai_at = case.stop_ai_at
        job.contact_source_at = case.contact_source_at
        job.pet_shield = case.pet_shield
        job.owner_attack_at = case.owner_attack_at
        job.is_fixture = case.is_fixture
        job.fixture_helper = case.fixture_helper
        job.fixture_helper_distance = case.fixture_helper_distance
        job.fixture_aoe = case.fixture_aoe
        job.fixture_pre_ms = case.fixture_pre_ms or 0
        job.fixture_post_ms = case.fixture_post_ms or 1500
        job.fixture_aura_exit = case.fixture_aura_exit
        job.fixture_contact = case.fixture_contact
        job.fixture_projectile = case.fixture_projectile
        job.is_projectile_lane = case.is_projectile_lane
        local heroNoun = case.hero or job.hero
        if living(job, job.victim).hero.noun ~= heroNoun then
            must(job.victim.World:ReplaceHero(heroNoun))
        end
        job.sourceOrigin = nil
        job.is_source_ai_disabled = false
        must(job.victim.World:PlaceHero(victimAnchor))
        must(job.observer.World:PlaceHero(observerAnchor))
        for _, client in ipairs({job.victim, job.observer}) do
            must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000,
                mana_point = case.mana_point}))
            must(client.World:ResetCooldowns())
        end
        if case.is_wall then
            local hero = living(job, job.victim).hero
            Contact.enter(job.observer, hero.position, 1.6,
                function(label, fields) log(job, label, fields) end)
        end
        run(job)
    end
end
return function()
    check(scenario.RequireAPI(1))
    if #jobs == 0 then
        check(scenario.Log("npc_phase_sweep suite_end cases=0 status=no_pending_capture"))
        return
    end
    local server = must(scenario.StartServer({}))
    local branches = {}
    local launchBranches = {}
    local caseCount = 0
    for _, job in ipairs(jobs) do
        caseCount = caseCount + (job.cases and #job.cases or 1)
    end
    check(scenario.Log("npc_phase_sweep suite_start cases=" .. caseCount .. " pairs=" .. #jobs))
    for _, job in ipairs(jobs) do
        launchBranches[job.id] = function() launchPair(server, job) end
    end
    check(scenario.Parallel(launchBranches))
    for _, job in ipairs(jobs) do
        prepareParty(job)
        branches[job.id] = function() prepare(job) end
    end
    check(scenario.Parallel(branches))
    local continueBranches = {}
    for _, job in ipairs(jobs) do
        continueBranches[job.id .. "-victim"] = function() must(job.victim.Mission:Continue()) end
        continueBranches[job.id .. "-observer"] = function() must(job.observer.Mission:Continue()) end
        branches[job.id] = function()
            prepareWorld(job)
            runBatch(job)
        end
    end
    check(scenario.Parallel(continueBranches))
    local failure = scenario.Parallel(branches)
    for _, job in ipairs(jobs) do
        check(job.victim:Terminate())
        check(job.observer:Terminate())
        check(job.victimProfile:Logout())
        check(job.observerProfile:Logout())
    end
    check(failure)
    check(scenario.Log("npc_phase_sweep suite_end cases=" .. caseCount .. " pairs=" .. #jobs))
end
