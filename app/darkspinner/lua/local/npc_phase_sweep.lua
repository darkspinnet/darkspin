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
    local maxCasts = math.max(1, math.min(10, job.fixture_max_casts or 3))
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
            local restored, restoreError = source.World:RestoreHero()
            if restoreError or not restored then
                return pending(emit, "completed prior cast resource restoration unavailable", castIndex - 1)
            end
            local reset, resetError = source.World:ResetCooldowns()
            if resetError or not reset or not reset.is_cooldown_reset or not reset.is_queued then
                return pending(emit, "completed prior cast cooldown reset unavailable", castIndex - 1)
            end
            emit("fixture_lethal_next_cast", {cast_index = castIndex, previous_dispatch_revision = previous,
                is_prior_authored_window_elapsed = true, is_producer_completion_observed = false})
        end
        local aim = {x = actor.position.x, y = actor.position.y,
            z = actor.position.z + (job.fixture_aim_z or cast.aim_z)}
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
    return pending(emit, "fixture remained alive after the bounded native casts", maxCasts)
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


-- Packet-confirmed cases are archived in the audit ledger; add only pending scopes here.
local jobs = {
    {id = "egg-fixture", hero = 185979955, cases = {
        {noun = "exploding_egg.Noun", is_poison_egg = true, poison_target_is_fixture = true},
    }},
    {id = "door", hero = 709733043, cases = {
        {noun = "design_blockin_door_destructible.Noun", is_fixture = true,
            fixture_post_ms = 11000, fixture_max_casts = 10},
    }},
    {id = "spoffit", hero = 576185321, cases = {
        {noun = "Spoffit.Noun", is_spoffit_crystal = true},
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
        hero_nouns = nouns, chain_progression = math.max(1, (job.mission or 1) - 1)}))
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
    must(job.victim.Mission:Select(job.mission or 1))
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

local function hazardGoal(job, client, center, radius, isLocalDestination)
    local origin = living(job, client).hero.position
    for _, direction in ipairs({{1,0}, {-1,0}, {0,1}, {0,-1},
        {0.7071,0.7071}, {-0.7071,0.7071}, {0.7071,-0.7071}, {-0.7071,-0.7071}}) do
        local point = {x = center.x + direction[1] * radius,
            y = center.y + direction[2] * radius, z = center.z}
        -- PlaceHero is an explicit teleport. Distant scenery needs a verified
        -- local destination, rather than a bounded route from mission spawn.
        local start = isLocalDestination and {x = point.x - direction[2],
            y = point.y + direction[1], z = point.z} or origin
        local path = must(client.World:Path(start, point))
        log(job, "hazard_path", {client = client.ID, radius = radius,
            is_local_destination = isLocalDestination == true, is_available = path.is_available,
            is_complete = path.is_complete, reason = path.unavailable_reason,
            goal_distance = path.goal and path.goal.distance,
            goal_x = path.goal and path.goal.position and path.goal.position.x,
            goal_y = path.goal and path.goal.position and path.goal.position.y,
            goal_z = path.goal and path.goal.position and path.goal.position.z})
        if path.is_available and path.is_complete and path.goal
            and distance(path.goal.position, point) <= 0.75
            and math.abs(distance(path.goal.position, center) - radius) <= 1 then
            return path.goal.position
        end
    end
    error("hazard placement lacks an authored-mesh route")
end

local function requireSilence(job, client, isExpected, stage)
    local deadline = scenario.Time() + 3000
    repeat
        local world = living(job, client)
        if world.hero.is_silenced == isExpected then
            local native = must(client.World:SnapshotNative()).gameplay_state
            if not native or not native.is_available or not native.is_world_active then
                error("silence observation lost native world")
            end
            log(job, "silence_state", {client = client.ID, stage = stage,
                is_silenced = world.hero.is_silenced, frame = native.frame_sequence,
                object = world.hero.object_id, hp = world.hero.hit_point, time = scenario.Time()})
            return
        end
        if scenario.Time() >= deadline then error("silence state disagreed at " .. stage) end
        check(scenario.Sleep(100))
    until false
end

local function runSilenceFixture(job)
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    job.is_source_ai_disabled = true
    local first = job.source
    local firstActor = must(first:Snapshot())
    log(job, "spawn", {noun = job.noun, object = firstActor.identity.object_id,
        game = living(job, job.victim).game_id, time = scenario.Time(),
        victim_pid = job.victim.PID, observer_pid = job.observer.PID})
    for _, client in ipairs({job.victim, job.observer}) do
        local contact = Contact.observe(client, firstActor.position, 11, 450,
            function(label, fields) log(job, label, fields) end)
        if not contact.is_confirmed then error("silence entry lacks native/server contact") end
        requireSilence(job, client, true, "first_entry")
    end
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    local second = must(job.source:Snapshot())
    log(job, "silence_overlap", {first = firstActor.identity.object_id,
        second = second.identity.object_id, time = scenario.Time()})
    check(scenario.Sleep(500))
    local retired = must(first:Retire())
    if not retired.is_accepted or not retired.is_settled then error("first silence source retirement failed") end
    for _, client in ipairs({job.victim, job.observer}) do requireSilence(job, client, true, "overlap_retained") end
    -- This exact installed noun has no combatant component. Verify its
    -- actual trigger lifetime through exit/reentry and explicit destruction.
    local entry = living(job, job.victim).hero.position
    local exit = hazardGoal(job, job.victim, second.position, 14)
    local outside = Contact.verifyPlacement(job.victim, exit, exit, 0.75,
        function(label, fields) log(job, label, fields) end)
    if not outside.is_confirmed then error("silence exit lacks native/server arrival") end
    requireSilence(job, job.victim, false, "exit")
    requireSilence(job, job.observer, true, "observer_still_inside")
    local inside = Contact.verifyPlacement(job.victim, entry, second.position, 11,
        function(label, fields) log(job, label, fields) end)
    if not inside.is_confirmed then error("silence reentry lacks native/server arrival") end
    requireSilence(job, job.victim, true, "reentry")
    finish(job, {object_id = second.identity.object_id})
    for _, client in ipairs({job.victim, job.observer}) do requireSilence(job, client, false, "destroyed") end
end

local function runPlasmaScenery(job)
    local world = living(job, job.victim)
    local source
    for _, hazard in ipairs(world.scenery_hazards or {}) do
        if string.lower(hazard.noun) == string.lower(job.noun) and hazard.ability == "CitadelPlasmaBurn" then
            source = hazard
            break
        end
    end
    if not source then error("selected mission has no published exact plasma scenery source") end
    log(job, "spawn", {noun = job.noun, object = source.object_id,
        game = world.game_id, is_selected_scenery = true, time = scenario.Time(),
        x = source.position.x, y = source.position.y, z = source.position.z,
        victim_pid = job.victim.PID, observer_pid = job.observer.PID})
    local safe = hazardGoal(job, job.observer, source.position, 14, true)
    local observerPlacement = Contact.verifyPlacement(job.observer, safe, safe, 0.75,
        function(label, fields) log(job, label, fields) end)
    if not observerPlacement.is_confirmed then error("plasma observer lacks native/server safe arrival") end
    local entry = hazardGoal(job, job.victim, source.position, 3, true)
    local contact = Contact.verifyPlacement(job.victim, entry, source.position, 5,
        function(label, fields) log(job, label, fields) end)
    if not contact.is_confirmed then error("plasma entry lacks native/server contact") end
    local before = living(job, job.victim).hero.hit_point
    local observed = Contact.observe(job.victim, source.position, 5, 4300,
        function(label, fields) log(job, label, fields) end)
    if not observed.is_confirmed then error("plasma contact did not remain confirmed") end
    local after = living(job, job.victim).hero.hit_point
    if after >= before then error("plasma contact produced no native-path damage") end
    log(job, "plasma_contact_damage", {object = source.object_id, before_hp = before, hp = after})
    local exit = hazardGoal(job, job.victim, source.position, 14, true)
    local outside = Contact.verifyPlacement(job.victim, exit, exit, 0.75,
        function(label, fields) log(job, label, fields) end)
    if not outside.is_confirmed then error("plasma exit lacks native/server arrival") end
    local exitHP = living(job, job.victim).hero.hit_point
    check(scenario.Sleep(1800))
    if living(job, job.victim).hero.hit_point ~= exitHP then error("plasma damage continued after confirmed exit") end
    log(job, "plasma_exit", {object = source.object_id, hp = exitHP, time = scenario.Time()})
    finish(job, {object_id = source.object_id})
end

local function runGeyserFixture(job)
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    job.is_source_ai_disabled = true
    local actor = must(job.source:Snapshot())
    local receipt = job.source.InitialSnapshot
    log(job, "spawn", {noun = job.noun, object = actor.identity.object_id,
        game = living(job, job.victim).game_id, time = scenario.Time(),
        victim_pid = job.victim.PID, observer_pid = job.observer.PID})
    local safe = hazardGoal(job, job.observer, actor.position, 14)
    local observerPlacement = Contact.verifyPlacement(job.observer, safe, safe, 0.75,
        function(label, fields) log(job, label, fields) end)
    if not observerPlacement.is_confirmed then error("geyser observer lacks native/server safe arrival") end
    if job.geyser_death then
        local lethal = Lethal.run(job, function(label, fields) log(job, label, fields) end)
        actor = must(job.source:Snapshot())
        if not lethal.is_confirmed or not actor.is_dead then error("blocked geyser lacks native lethal cast") end
        log(job, "geyser_unblocked", {object = actor.identity.object_id, time = scenario.Time()})
    end
    local origin = living(job, job.victim).hero.position
    local path = must(job.victim.World:Path(origin, actor.position))
    if not path.is_available or not path.is_complete or not path.goal or not path.footprint_radius then
        error("geyser contact lacks a complete native-footprint mesh route")
    end
    local limit = 0.1 + path.footprint_radius
    local contact = Contact.verifyPlacement(job.victim, path.goal.position, actor.position, limit,
        function(label, fields) log(job, label, fields) end)
    if not contact.is_confirmed then error("geyser contact lacks native/server arrival") end
    local before = living(job, job.victim).hero.hit_point
    local observed = Contact.observe(job.victim, actor.position, limit, 11000,
        function(label, fields) log(job, label, fields) end)
    if not observed.is_confirmed then error("geyser contact did not remain confirmed") end
    local after = living(job, job.victim).hero.hit_point
    if after >= before then error("geyser contact produced no native-path spout damage") end
    log(job, "geyser_contact_damage", {object = actor.identity.object_id,
        before_hp = before, hp = after, limit = limit, time = scenario.Time()})
    finish(job, receipt)
end

local function castPoisonEgg(job, targetID, index, existingSource)
    local source = existingSource or must(job.victim.World:SpawnControlledFixture(job.noun))
    local actor = must(source:Snapshot())
    local target = must(job.ally:Snapshot())
    if distance(actor.position, target.position) >= 1.5 then error("egg recipient is outside exact query radius") end
    job.source = source
    job.is_source_ai_disabled = true
    job.fixture_aim_z = 1.8
    job.fixture_max_casts = 1
    log(job, "spawn", {noun = job.noun, object = actor.identity.object_id, target = targetID,
        game = living(job, job.victim).game_id, index = index, time = scenario.Time(),
        victim_pid = job.victim.PID, observer_pid = job.observer.PID})
    local lethal = Lethal.run(job, function(label, fields) log(job, label, fields) end)
    local dispatch = living(job, job.victim).ability.last_dispatch
    local dead = must(source:Snapshot())
    if not lethal.is_confirmed or not dead.is_dead or not dispatch then
        error("egg lacks a confirmed native cast and actual source death")
    end
    job.source = nil
    log(job, "egg_native_death", {object = actor.identity.object_id, target = targetID,
        index = index, revision = dispatch.revision, dispatch_target = dispatch.target_object_id,
        is_selected_source = dispatch.target_object_id == actor.identity.object_id,
        time = scenario.Time()})
    return {source = source, object_id = actor.identity.object_id}
end

local function runPoisonEgg(job)
    local initialEgg
    if job.poison_target_is_fixture then
        initialEgg = must(job.victim.World:SpawnControlledFixture(job.noun))
        local actor = must(initialEgg:Snapshot())
        local origin = living(job, job.victim).hero.position
        local requested = {x = actor.position.x - 5, y = actor.position.y + 1.1, z = actor.position.z}
        local path = must(job.victim.World:Path(origin, requested))
        if not path.is_available or not path.is_complete or not path.goal
            or distance(path.goal.position, requested) > 0.4 then
            error("fixture recipient setup lacks a confirmed mesh route")
        end
        local placement = Contact.verifyPlacement(job.victim, path.goal.position, path.goal.position, 0.75,
            function(label, fields) log(job, label, fields) end)
        if not placement.is_confirmed then error("fixture recipient setup lacks native/server arrival") end
        job.ally = must(job.victim.World:SpawnControlledFixture("DEST_cryos_stalagmite_small_destructible.Noun"))
    else
        job.ally = must(job.victim.World:SpawnControlledNPC("Critter.Noun"))
        must(job.ally:SetAI(true))
        check(scenario.Sleep(3500))
        must(job.ally:SetAI(false))
    end
    local target = must(job.ally:Snapshot())
    local world = living(job, job.victim)
    local requested = {x = target.position.x - 5,
        y = target.position.y + (job.poison_target_is_fixture and -1.1 or 1.1), z = target.position.z}
    local path = must(job.victim.World:Path(world.hero.position, requested))
    if not path.is_available or not path.is_complete or not path.goal
        or distance(path.goal.position, requested) > 0.4 then
        error("egg caster setup lacks a confirmed mesh route")
    end
    local placement = Contact.verifyPlacement(job.victim, path.goal.position, path.goal.position, 0.75,
        function(label, fields) log(job, label, fields) end)
    if not placement.is_confirmed then error("egg caster setup lacks native/server arrival") end
    log(job, "egg_recipient", {object = target.identity.object_id, hp = target.hit_point,
        is_fixture = job.poison_target_is_fixture == true, time = scenario.Time()})
    local eggs = {castPoisonEgg(job, target.identity.object_id, 1, initialEgg)}
    if job.egg_refresh then
        must(job.victim.World:ResetCooldowns())
        eggs[#eggs + 1] = castPoisonEgg(job, target.identity.object_id, 2)
    end
    if job.egg_recipient_death then
        local damageJob = {victim = job.victim, observer = job.observer, source = job.ally}
        local lethal = Lethal.run(damageJob, function(label, fields) log(job, label, fields) end)
        local dead = must(job.ally:Snapshot())
        if not lethal.is_confirmed or not dead.is_dead then error("poison recipient lacks a confirmed native death") end
        log(job, "egg_recipient_death", {object = target.identity.object_id, time = scenario.Time()})
    end
    for index = 1, 7 do
        check(scenario.Sleep(1000))
        local actor = must(job.ally:Snapshot())
        log(job, "egg_poison_sample", {object = target.identity.object_id,
            hp = actor.hit_point, is_dead = actor.is_dead, sample = index, time = scenario.Time()})
        for _, client in ipairs({job.victim, job.observer}) do living(job, client) end
    end
    for _, egg in ipairs(eggs) do
        local retired = must(egg.source:Retire())
        if not retired.is_accepted or not retired.is_settled then error("settled egg source retirement failed") end
    end
    finish(job, {object_id = eggs[1].object_id})
end

local function runSpoffitCrystal(job)
    local owner = living(job, job.victim).hero
    local receipt = must(job.victim.World:ActivateSpoffitCrystal())
    if not receipt.object_id or receipt.object_id == 0 then error("Spoffit creator has no published child") end
    log(job, "spawn", {noun = job.noun, object = receipt.object_id, owner = owner.object_id,
        game = living(job, job.victim).game_id, time = scenario.Time(),
        victim_pid = job.victim.PID, observer_pid = job.observer.PID})
    check(scenario.Sleep(2300))
    for index, offset in ipairs({{8, 0}, {0, 8}}) do
        local world = living(job, job.victim)
        local goal
        for _, sign in ipairs({1, -1}) do
            local requested = {x = world.hero.position.x + offset[1] * sign,
                y = world.hero.position.y + offset[2] * sign, z = world.hero.position.z}
            local path = must(job.victim.World:Path(world.hero.position, requested))
            if path.is_available and path.is_complete and path.goal
                and distance(path.goal.position, requested) <= 0.75 then
                goal = path.goal.position
                break
            end
        end
        if not goal then error("Spoffit owner movement has no complete mesh route") end
        local placement = Contact.verifyPlacement(job.victim, goal, goal, 0.75,
            function(label, fields) log(job, label, fields) end)
        if not placement.is_confirmed then error("Spoffit owner movement lacks native/server arrival") end
        log(job, "spoffit_owner_move", {object = receipt.object_id, owner = owner.object_id,
            x = goal.x, y = goal.y, z = goal.z, index = index, time = scenario.Time()})
        check(scenario.Sleep(3000))
        for _, client in ipairs({job.victim, job.observer}) do
            local current = living(job, client)
            local native = must(client.World:SnapshotNative()).gameplay_state
            if not native or not native.is_available or not native.is_world_active then
                error("Spoffit follow lost native world")
            end
            log(job, "spoffit_follow_window", {object = receipt.object_id, client = client.ID,
                frame = native.frame_sequence, hp = current.hero.hit_point, index = index})
        end
    end
    log(job, "spoffit_deactivate", {object = receipt.object_id, time = scenario.Time()})
    local retired = must(job.victim.World:DeactivateSpoffitCrystal())
    log(job, "spoffit_retired", {object = retired.object_id, state = retired.state, time = scenario.Time()})
    finish(job, receipt)
end
local ProjectileLane = {}
ProjectileLane.__index = ProjectileLane

function ProjectileLane:outsideFoundation(center, target, caster)
    local targetX, casterX = target.x - center.x, caster.x - center.x
    local targetY, casterY = target.y - center.y, caster.y - center.y
    return math.min(targetX, casterX) >= 6 or math.max(targetX, casterX) <= -6
        or math.min(targetY, casterY) >= 6.5 or math.max(targetY, casterY) <= -6.5
end

function ProjectileLane:inspectPath(client, owner, start, goal, candidateIndex, stage)
    local path, pathError = client.World:Path(start, goal)
    local isIdentity = matches(path, owner) == true
    local goalDistance = path and path.goal and path.goal.position and distance(path.goal.position, goal)
    local startDistance = path and path.start and path.start.position and distance(path.start.position, start)
    local reason = pathError and tostring(pathError) or not path and "missing_receipt"
        or not path.is_available and "unavailable" or not isIdentity and "identity_mismatch"
        or not path.is_complete and "incomplete" or path.is_truncated and "truncated"
        or not goalDistance and "missing_goal" or goalDistance > 0.8 and "goal_projection_miss"
        or not startDistance and "missing_start" or startDistance > 0.8 and "start_projection_miss"
        or "accepted"
    log(self.job, "projectile_lane_candidate_path", {noun = self.job.noun, candidate = candidateIndex,
        stage = stage, reason = reason, is_available = path and path.is_available or false,
        is_complete = path and path.is_complete or false, is_truncated = path and path.is_truncated or false,
        is_identity_matching = isIdentity, goal_projection_distance = goalDistance or -1,
        start_projection_distance = startDistance or -1, requested_x = goal.x, requested_y = goal.y,
        projected_x = path and path.goal and path.goal.position.x,
        projected_y = path and path.goal and path.goal.position.y,
        unavailable_reason = path and path.unavailable_reason,
        error_code = pathError and pathError.Code, error_context = pathError and pathError.Context,
        error_message = pathError and pathError.Message})
    if reason ~= "accepted" then return nil end
    return path
end

function ProjectileLane:plan(center, helper)
    local caster = living(self.job, self.job.observer)
    local owner = living(self.job, self.job.victim)
    -- Vertical lanes clear the foundation's width; horizontal lanes clear its depth.
    for candidateIndex, offset in ipairs({{6,4,true}, {-6,4,true}, {6,5,true}, {-6,5,true},
        {6,6,true}, {-6,6,true}, {7,4,true}, {-7,4,true}, {7,5,true}, {-7,5,true},
        {4,7}, {-4,7}, {5,7}, {-5,7}, {4,-7}, {-4,-7},
        {6,7}, {6,-7}, {0,8}}) do
        local targetPoint = {x = center.x + offset[1], y = center.y + offset[2], z = center.z}
        local casterPoint = {x = offset[3] and targetPoint.x or center.x - (offset[1] == 0 and 8 or offset[1]),
            y = center.y + (offset[3] and -offset[2] or offset[2]), z = center.z}
        local targetPath = self:inspectPath(self.job.victim, owner, helper.position, targetPoint, candidateIndex, "target")
        local casterPath = self:inspectPath(self.job.observer, caster, caster.hero.position, casterPoint, candidateIndex, "caster")
        if targetPath and casterPath then
            targetPoint, casterPoint = targetPath.goal.position, casterPath.goal.position
            local lanePath = self:inspectPath(self.job.observer, caster, casterPoint, targetPoint, candidateIndex, "flight")
            local length = 0
            if lanePath and lanePath.points then
                for index = 2, #lanePath.points do
                    length = length + distance(lanePath.points[index - 1], lanePath.points[index])
                end
            end
            local direct = distance(casterPoint, targetPoint)
            local isStraight = length >= direct - 0.8 and length <= direct + 0.8
            local isOutside = self:outsideFoundation(center, targetPoint, casterPoint)
            local isInAura = distance(targetPoint, center) <= 12 and distance(casterPoint, center) <= 12
            log(self.job, "projectile_lane_candidate_geometry", {candidate = candidateIndex,
                is_path_available = lanePath ~= nil, is_straight = isStraight, is_outside_foundation = isOutside,
                is_in_aura = isInAura, direct_length = direct, path_length = length})
            if lanePath and isStraight and direct >= 7 and direct <= 14
                and distance(targetPoint, center) <= 12 and distance(casterPoint, center) <= 12
                and isOutside then
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
        is_confirmed = not reason and self.cast_count == (self.job.maximum_casts or 12)})
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
    if not lane:outsideFoundation(fixture.position, target.position, point)
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
        caster_x = point.x, caster_y = point.y, maximum_casts = job.maximum_casts or 12})
    for index = 1, job.maximum_casts or 12 do
        local isConfirmed, reason, isInputUnknown = lane:cast(index)
        if not isConfirmed then lane:finish(reason, isInputUnknown); return end
    end
    local finalFixture = must(job.source:Snapshot())
    if finalFixture.is_dead then lane:finish("projectile lane fixture did not remain alive", false); return end
    lane:finish()
end
local function runOwnedDroid(job)
    job.source = must(job.victim.World:SpawnControlledNPC("NomadCyberOne.Noun"))
    job.is_source_ai_disabled = true
    local parent = must(job.source:Snapshot())
    if parent.is_ai_enabled or parent.is_dead then error("owned droid parent is not idle and alive") end
    local receipt = must(job.victim.World:SpawnOwnedNPC(job.noun, job.source))
    if receipt.owner_object_id ~= parent.identity.object_id or not receipt.is_owner_support_started then
        error("owned droid birth did not bind its real support owner")
    end
    log(job, "spawn", {noun = job.noun, object = receipt.object_id, owner = receipt.owner_object_id,
        game = receipt.game_id, is_owner_support_started = receipt.is_owner_support_started,
        victim_pid = job.victim.PID, observer_pid = job.observer.PID, time = scenario.Time()})
    -- The AI-off owner retains its authored invisible pre-aggro state. Let
    -- its normal entrance play after the exact child is bound, then freeze it.
    must(job.source:SetAI(true))
    check(scenario.Sleep(3500))
    must(job.source:SetAI(false))
    local visibleParent = must(job.source:Snapshot())
    local requested = {x = visibleParent.position.x - 3, y = visibleParent.position.y, z = visibleParent.position.z}
    local path = must(job.victim.World:Path(visibleParent.position, requested))
    if not path.is_available or not path.is_complete or not path.goal
        or distance(path.goal.position, requested) > 0.8 then
        error("owned droid parent separation has no confirmed route")
    end
    local moved = must(job.source:Move(path.goal.position))
    if not moved.is_completed then error("owned droid parent separation did not settle") end
    log(job, "owned_droid_owner_entrance", {owner = parent.identity.object_id,
        child = receipt.object_id, is_ai_enabled = false, time = scenario.Time()})
    local before = living(job, job.observer)
    local revision = before.ability.last_dispatch and before.ability.last_dispatch.revision or 0
    hitOwner(job, parent.identity.object_id, job.observer)
    local deadline = scenario.Time() + 2000
    local isDispatched = false
    repeat
        local current = living(job, job.observer)
        local dispatch = current.ability.last_dispatch
        if dispatch and dispatch.revision > revision then
            if dispatch.revision ~= revision + 1 or dispatch.actor_object_id ~= current.hero.object_id
                or dispatch.target_object_id ~= parent.identity.object_id or dispatch.ability_asset ~= "FieldMedicBasic"
                or dispatch.ability_id ~= 76539968 or dispatch.slot ~= "basic" then
                error("owned droid damage input disagreed; no replay")
            end
            isDispatched = true
            log(job, "owned_droid_damage_dispatch", {owner = parent.identity.object_id,
                child = receipt.object_id, revision = dispatch.revision, ability_id = dispatch.ability_id})
            break
        end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    if not isDispatched then error("owned droid lacks a fresh native damage dispatch; no replay") end
    for index = 1, 12 do
        check(scenario.Sleep(1000))
        local actor = must(job.source:Snapshot())
        if not actor.is_available or actor.is_dead or actor.is_ai_enabled then
            error("owned droid parent changed during healing window")
        end
        for _, client in ipairs({job.victim, job.observer}) do
            local world = living(job, client)
            local isChildFound = false
            for _, npc in ipairs(world.npcs) do
                if npc.object_id == receipt.object_id then
                    isChildFound = true
                    log(job, "owned_droid_child", {client = client.ID, object = npc.object_id,
                        noun = npc.noun, owner = npc.owner_object_id, hp = npc.hit_point, sample = index})
                end
            end
            if not isChildFound then error("owned droid disappeared before healing window ended") end
            log(job, "owned_droid_owner", {client = client.ID, object = parent.identity.object_id,
                hp = actor.hit_point, sample = index, is_ai_enabled = actor.is_ai_enabled})
        end
    end
    finish(job, receipt)
    check(scenario.Sleep(1500))
    for _, client in ipairs({job.victim, job.observer}) do
        local world = living(job, client)
        for _, npc in ipairs(world.npcs) do
            if npc.object_id == parent.identity.object_id or npc.object_id == receipt.object_id then
                error("owned droid source retirement did not remove its child")
            end
        end
    end
    log(job, "owned_droid_cleanup", {child = receipt.object_id, owner = parent.identity.object_id,
        is_confirmed = true, time = scenario.Time()})
end

local function runPassiveFixture(job)
    job.source = must(job.victim.World:SpawnControlledFixture(job.noun))
    job.is_source_ai_disabled = true
    local receipt = job.source.InitialSnapshot
    log(job, "spawn", {noun = job.noun, object = receipt.object_id, game = receipt.game_id,
        victim_pid = job.victim.PID, observer_pid = job.observer.PID, time = scenario.Time()})
    for index = 1, job.sample_count do
        check(scenario.Sleep(1000))
        local actor = fixtureSample(job, "fixture_passive")
        if actor.is_dead then error("passive fixture died before its capture window ended") end
        log(job, "passive_fixture_sample", {noun = job.noun, object = receipt.object_id, sample = index})
    end
    finish(job, receipt)
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
    if job.is_spoffit_crystal then runSpoffitCrystal(job); return end
    if job.is_poison_egg then runPoisonEgg(job); return end
    if job.is_geyser_fixture then runGeyserFixture(job); return end
    if job.is_silence_fixture then runSilenceFixture(job); return end
    if job.is_plasma_scenery then runPlasmaScenery(job); return end
    if job.is_owned_droid then runOwnedDroid(job); return end
    if job.is_passive_fixture then runPassiveFixture(job); return end
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
        job.is_owned_droid = case.is_owned_droid
        job.is_spoffit_crystal = case.is_spoffit_crystal
        job.is_poison_egg = case.is_poison_egg
        job.poison_target_is_fixture = case.poison_target_is_fixture
        job.egg_refresh = case.egg_refresh
        job.egg_recipient_death = case.egg_recipient_death
        job.fixture_aim_z = case.fixture_aim_z
        job.fixture_max_casts = case.fixture_max_casts
        job.is_geyser_fixture = case.is_geyser_fixture
        job.geyser_death = case.geyser_death
        job.is_silence_fixture = case.is_silence_fixture
        job.is_plasma_scenery = case.is_plasma_scenery
        job.is_passive_fixture = case.is_passive_fixture
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
        job.maximum_casts = case.maximum_casts or 12
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
