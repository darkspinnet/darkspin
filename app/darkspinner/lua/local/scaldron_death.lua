---Build-103 Scaldron death jobs through one real co-op boss lifetime.
local must, check = scenario.Must, scenario.Check
local Battle = {}
Battle.__index = Battle

-- Empty ordinary spawn room; do not enter the authored Scaldron boss arena.
local mission = 1
local arena
-- Installed Char Delta basic, chunk459: range30, radius0.5, weapon damage.
local basic = {asset = "FireTempestBasic", id = 75763312, slot = "basic"}
local stageOne, stageTwo = "scaldronboss.noun", "scaldronboss_stage2.noun"

local function log(label, fields)
    local parts = {"scaldron_death", label, "time=" .. scenario.Time()}
    for name, entry in pairs(fields or {}) do parts[#parts + 1] = name .. "=" .. tostring(entry) end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end

local function distance(a, b)
    local dx, dy, dz = a.x-b.x, a.y-b.y, a.z-b.z
    return math.sqrt(dx*dx+dy*dy+dz*dz)
end

local function actor(world, objectID)
    for _, npc in ipairs(world.npcs or {}) do if npc.object_id == objectID then return npc end end
    return nil
end

function Battle:capture(label)
    for _, client in ipairs(self.clients) do
        local capture, captureError = client.Diagnostics:Capture(label)
        log("capture", {client = client.ID, pid = client.PID, boss = self.bossID,
            label = label, manifest = capture and capture.manifest_path, error = captureError})
    end
end

function Battle:world(client)
    local world = must(client.World:SnapshotLiving())
    local epoch = self.epochs[client.ID]
    if epoch and (world.game_id ~= epoch.game_id or world.zone_generation ~= epoch.zone_generation
        or world.peer_generation ~= epoch.peer_generation or world.transport_generation ~= epoch.transport_generation
        or world.hero.object_id ~= epoch.hero_object_id) then error("owned world/hero epoch changed") end
    if world.hero.noun ~= 709733043 then error("equipped Char Delta slot changed") end
    return world
end

function Battle:sample(label)
    local result
    for _, client in ipairs(self.clients) do
        local world = self:world(client)
        local probe, probeError = client.World:SnapshotNative()
        local native = probe and probe.gameplay_state
        if probe and (probe.pid ~= client.PID or probe.instance_id ~= client.InstanceID) then error("native process identity changed") end
        local flags = {phase = label, client = client.ID, is_native_record_present = native ~= nil}
        for name, entry in pairs(native or {}) do if type(entry) == "boolean" then flags[name] = entry end end
        log("native_flags", flags)
        if world.boss then
            log("boss_state", {phase = label, client = client.ID, boss_phase = world.boss.phase,
                leader = world.boss.leader_object_id, leader_hp = world.boss.leader_hit_point,
                is_complete = world.boss.is_complete})
        end
        local position = world.hero.is_movement_available and world.hero.movement_position or world.hero.position
        log("hero", {phase = label, client = client.ID, pid = client.PID,
            hero = world.hero.object_id, hp = world.hero.hit_point, frame = native and native.frame_sequence,
            x = position.x, y = position.y, z = position.z, is_movement_available = world.hero.is_movement_available,
            native_x = native and native.position and native.position.x,
            native_y = native and native.position and native.position.y,
            native_z = native and native.position and native.position.z,
            is_native_available = native and native.is_available, is_world_active = native and native.is_world_active,
            is_rig_ready = native and native.is_rig_ready, native_error = probeError,
            is_native_living_confirmed = native ~= nil and native.is_available == true
                and native.is_world_active == true and native.is_rig_ready == true})
        for _, npc in ipairs(world.npcs or {}) do
            if distance(npc.position, arena) <= 90 or npc.object_id == self.bossID then
                log("actor", {phase = label, client = client.ID, object = npc.object_id, noun = npc.noun,
                    hp = npc.hit_point, is_dead = npc.is_dead, is_published = npc.is_published,
                    is_fixture = npc.is_fixture, x = npc.position.x, y = npc.position.y, z = npc.position.z,
                    is_owner_identity_available = false})
            end
        end
        if client == self.caster then result = world end
    end
    return result
end

function Battle:waitRig(client, isControlRequired)
    local deadline, previousFrame, frames = scenario.Time()+30000, 0, 0
    repeat
        local world = self:world(client)
        local probe, probeError = client.World:SnapshotNative()
        local native = probe and probe.gameplay_state
        if probe and (probe.pid ~= client.PID or probe.instance_id ~= client.InstanceID) then error("native readiness process changed") end
        local isControlReady = not isControlRequired or (world.hero.is_stunned == false
            and world.hero.is_silenced == false and world.hero.input_lock_remaining_ms <= 0)
        if native and native.is_available and native.is_world_active and native.is_rig_ready
            and native.frame_sequence > previousFrame and isControlReady then frames = frames+1 else frames = 0 end
        previousFrame = native and native.frame_sequence or previousFrame
        log("native_readiness", {client = client.ID, frame = previousFrame, frames = frames,
            is_native_available = native and native.is_available, is_world_active = native and native.is_world_active,
            is_rig_ready = native and native.is_rig_ready, is_control_ready = isControlReady,
            error = probeError, is_control_required = isControlRequired})
        if frames >= 2 then return native end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    error("native rig/control unavailable before bounded readiness deadline")
end

function Battle:place(client, requested)
    local world = self:world(client)
    local path = must(client.World:Path(requested, requested))
    if not path.is_available or not path.is_complete or not path.goal
        or distance(path.goal.position, requested) > 0.75 then error("arena placement has no exact mesh projection") end
    local receipt = must(client.World:PlaceHero(path.goal.position))
    log("placement_queued", {client = client.ID, x = path.goal.position.x,
        y = path.goal.position.y, z = path.goal.position.z, receipt = tostring(receipt.is_queued)})
    local deadline, frames, previous = scenario.Time()+4000, 0, 0
    repeat
        world = self:world(client)
        local native = must(client.World:SnapshotNative()).gameplay_state
        local server = world.hero.is_movement_available and world.hero.movement_position or world.hero.position
        if native and native.is_available and native.position and native.frame_sequence > previous
            and distance(native.position, path.goal.position) <= 0.8
            and distance(server, path.goal.position) <= 0.8 then frames = frames+1 else frames = 0 end
        previous = native and native.frame_sequence or 0
        if frames >= 2 then
            log("placement_confirmed", {client = client.ID, frame = previous,
                x = native.position.x, y = native.position.y, z = native.position.z})
            return
        end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error("native/server placement did not agree")
end

function Battle:cast(objectID, label, castIndex)
    self:waitRig(self.caster, true)
    local target = actor(self:world(self.caster), objectID)
    if not target or target.is_dead then error("cast target unavailable before input") end
    if not self.isInputCaptured then
        self:capture("scaldron-death-before-input")
        self.isInputCaptured = true
        target = actor(self:world(self.caster), objectID)
        if not target or target.is_dead then return false end
    end
    target = actor(self:world(self.caster), objectID)
    if not target or target.is_dead then return false end
    local aim = {x = target.position.x, y = target.position.y, z = target.position.z+0.3}
    local projected, projectionError = self.caster.World:Project(aim)
    local projection = projected and projected.projection
    if projectionError or not projection or not projection.is_available or not projection.is_on_screen
        or projection.y >= 620 or projection.x <= 30 then
        self.prepressStartedAt = nil
        log("no_input_attempt", {phase = label, cast = castIndex, target = objectID,
            is_cast_confirmed = false, is_input_submitted = false, reason = "target projection unavailable or in HUD",
            error = projectionError, x = projection and projection.x, y = projection and projection.y})
        check(scenario.Sleep(1000))
        self:sample(label .. "_no_input_" .. castIndex)
        return false, "deferred"
    end
    log("aim_projection", {phase = label, target = objectID,
        screen_x = projection.screen_x, screen_y = projection.screen_y,
        x = projection.x, y = projection.y, is_on_screen = projection.is_on_screen})
    local before = self:world(self.caster)
    local previous = before.ability.last_dispatch and before.ability.last_dispatch.revision or 0
    log("native_input_before", {phase = label, cast = castIndex, target = objectID,
        before_hp = target.hit_point, previous_revision = previous, asset = basic.asset,
        ability_id = basic.id, aim_x = aim.x, aim_y = aim.y, aim_z = aim.z})
    local receipt, inputError = self.caster.World:Click(aim)
    local click = receipt and receipt.gameplay_click
    local inputFields = {phase = label, target = objectID, error = inputError,
        status = receipt and receipt.status, is_processed = receipt and receipt.is_processed}
    for name, entry in pairs(click or {}) do
        if type(entry) ~= "table" then inputFields[name] = entry end
    end
    log("native_picker_receipt", inputFields)
    if receipt and receipt.status == "unavailable" and click and click.is_projected == true
        and click.is_press_poll_observed == false and click.is_release_poll_observed == false then
        self.prepressStartedAt = self.prepressStartedAt or scenario.Time()
        log("native_prepress_refusal", {phase = label, cast = castIndex, target = objectID,
            status = receipt.status, is_cast_confirmed = false, is_input_submitted = false,
            consecutive_wait_ms = scenario.Time()-self.prepressStartedAt,
            reason = "explicit unavailable before either input poll"})
        if scenario.Time()-self.prepressStartedAt >= 30000 then error("physical input gate unavailable for thirty consecutive seconds") end
        check(scenario.Sleep(400))
        return false, "deferred"
    end
    self.prepressStartedAt = nil
    if inputError or not receipt or not receipt.is_processed or not click
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        self:capture("scaldron-death-input-unknown")
        error("native basic edge unknown; no replay")
    end
    local dispatchDeadline = scenario.Time()+2000
    local isCursorTarget = false
    repeat
        local current = self:world(self.caster).ability.last_dispatch
        if current and current.revision > previous then
            if current.ability_asset ~= basic.asset or current.ability_id ~= basic.id or current.slot ~= basic.slot then
                self:capture("scaldron-death-dispatch-mismatch")
                error("native basic dispatch identity differs; no replay")
            end
            if current.target_object_id ~= objectID and current.target_object_id ~= 0 then
                log("native_collateral_dispatch", {phase = label, cast = castIndex, target = objectID,
                    dispatch_target = current.target_object_id, revision = current.revision,
                    previous_revision = previous, asset = current.ability_asset,
                    ability_id = current.ability_id, slot = current.slot,
                    is_cast_confirmed = true, is_intended_target = false})
                check(scenario.Sleep(700))
                self:sample(label .. "_collateral_" .. castIndex)
                return false, "collateral"
            end
            isCursorTarget = current.target_object_id == 0
            log("native_dispatch", {phase = label, cast = castIndex, target = objectID,
                revision = current.revision, previous_revision = previous,
                asset = current.ability_asset, ability_id = current.ability_id, slot = current.slot,
                dispatch_target = current.target_object_id, is_cursor_target = isCursorTarget,
                is_single_revision = current.revision == previous+1})
            break
        end
        if scenario.Time() >= dispatchDeadline then
            log("native_dispatch_unconfirmed", {phase = label, cast = castIndex, target = objectID,
                previous_revision = previous, is_cast_confirmed = false,
                is_input_poll_pair_confirmed = true, reason = "no fresh dispatch; normal rejection may apply"})
            check(scenario.Sleep(700))
            self:waitRig(self.caster, false)
            self:sample(label .. "_unconfirmed_" .. castIndex)
            return false
        end
        check(scenario.Sleep(100))
    until false
    check(scenario.Sleep(700))
    if isCursorTarget then
        local after = actor(self:world(self.caster), objectID)
        if not after or (after.noun == target.noun and after.hit_point >= target.hit_point) then
            error("cursor basic has no correlated target damage/phase change; no replay")
        end
        log("cursor_target_confirmed", {object = objectID, before_hp = target.hit_point,
            hp = after.hit_point, before_noun = target.noun, noun = after.noun})
    end
    self:sample(label .. "_cast_" .. castIndex)
    return true
end

function Battle:bossTarget()
    local target = actor(self:world(self.caster), self.bossID)
    if target then
        local noun = string.lower(target.noun)
        if noun ~= stageOne and noun ~= stageTwo then error("boss noun left its actual two-stage lifetime") end
    end
    return target
end

function Battle:killBoss(stage)
    local castLimit, minimumObservedDamage, consecutiveZeroDamage = 8, nil, 0
    for castIndex = 1, 200 do
        if castIndex > castLimit then error("boss has no sufficient observed native damage budget") end
        local target = self:bossTarget()
        if not target then error("boss record absent; absence does not prove death") end
        if stage == 1 and string.lower(target.noun) == stageTwo then
            log("stage1_transition", {object = self.bossID, noun = target.noun, hp = target.hit_point})
            return
        end
        if stage == 2 and target.is_dead and target.hit_point <= 0 then
            log("stage2_lethal", {object = self.bossID, noun = target.noun, hp = target.hit_point})
            return
        end
        must(self.caster.World:RestoreHero())
        must(self.caster.World:ResetCooldowns())
        local isCastConfirmed, outcome = self:cast(self.bossID, "stage" .. stage, castIndex)
        local after = self:bossTarget()
        if after and after.noun == target.noun and after.hit_point > 0 then
            local damage = target.hit_point-after.hit_point
            if isCastConfirmed and damage > 0 then
                consecutiveZeroDamage = 0
                minimumObservedDamage = math.min(minimumObservedDamage or damage, damage)
                local estimate = castIndex+math.ceil(after.hit_point/minimumObservedDamage)+2
                log("damage_budget", {phase = stage, cast = castIndex, hp = after.hit_point,
                    minimum_observed_damage = minimumObservedDamage, estimated_total_casts = estimate, hard_limit = 200})
                castLimit = math.min(200, math.max(castLimit, estimate))
            elseif outcome == "deferred" then
                castLimit = math.min(200, castLimit+1)
                log("damage_attempt_deferred", {phase = stage, cast = castIndex,
                    is_cast_confirmed = false, is_input_submitted = false, is_damage_budget_eligible = false})
            elseif outcome == "collateral" then
                castLimit = math.min(200, castLimit+1)
                log("collateral_damage_attempt", {phase = stage, cast = castIndex,
                    is_cast_confirmed = true, is_intended_target = false,
                    observed_boss_hp_delta = damage, is_damage_budget_eligible = false})
            else
                consecutiveZeroDamage = consecutiveZeroDamage+1
                castLimit = math.min(200, castLimit+1)
                log("zero_damage_attempt", {phase = stage, cast = castIndex,
                    is_cast_confirmed = isCastConfirmed, consecutive = consecutiveZeroDamage,
                    observed_hp_delta = damage, is_damage_budget_eligible = false, hp = after.hit_point})
                if consecutiveZeroDamage >= 5 then error("five consecutive completed native attempts produced no boss damage") end
            end
        end
    end
    local target = self:bossTarget()
    if target and ((stage == 1 and string.lower(target.noun) == stageTwo)
        or (stage == 2 and target.is_dead and target.hit_point <= 0)) then return end
    error("boss native lethal preparation reached intentional cast bound")
end

function Battle:portalTargets(world)
    local targets = {}
    for _, npc in ipairs(world.npcs or {}) do
        if string.lower(npc.noun) == "enemyportal.noun" and not npc.is_dead and npc.hit_point > 0
            and distance(npc.position, arena) <= 65 then targets[#targets+1] = npc end
    end
    table.sort(targets, function(a,b) return a.object_id < b.object_id end)
    return targets
end

function Battle:clearPortals()
    -- These are real observed portal bodies. Native OwnerIDs are audited offline.
    for round = 1, 32 do
        local targets = self:portalTargets(self:world(self.caster))
        if #targets == 0 then log("portal_gate_candidates_clear", {round = round}); return end
        for _, target in ipairs(targets) do
            log("portal_target", {object = target.object_id, hp = target.hit_point, is_owner_identity_available = false})
            self:cast(target.object_id, "portal", round)
        end
    end
    if #self:portalTargets(self:world(self.caster)) ~= 0 then error("native portal damage reached intentional bound") end
end

function Battle:waitStageTwo()
    local deadline = scenario.Time()+45000
    repeat
        local world = self:sample("stage2_wait")
        local boss = actor(world, self.bossID)
        local native = must(self.caster.World:SnapshotNative()).gameplay_state
        local elapsed = scenario.Time()-self.stageOneAt
        if boss and string.lower(boss.noun) == stageTwo and boss.hit_point > 0 and not boss.is_dead
            and elapsed >= 21000 and native and native.is_rig_ready
            and world.hero.input_lock_remaining_ms <= 0 and not world.hero.is_stunned then
            log("stage2_entry_observed", {object = self.bossID, hp = boss.hit_point,
                elapsed_ms = elapsed, is_native_attack_eligibility_proven = false})
            return
        end
        check(scenario.Sleep(1000))
    until scenario.Time() >= deadline
    error("normal stage2 entry remained unavailable after real portal clearing")
end

function Battle:waitMinions(label, baseline)
    local deadline = scenario.Time()+18000
    repeat
        local world = self:sample(label)
        local candidates = 0
        for _, npc in ipairs(world.npcs or {}) do
            if not baseline[npc.object_id] and npc.object_id ~= self.bossID and not npc.is_boss
                and not npc.is_fixture and not npc.is_dead and npc.hit_point > 0
                and distance(npc.position, arena) <= 65 then
                candidates = candidates+1
                log("live_minion_candidate", {phase = label, object = npc.object_id, noun = npc.noun,
                    hp = npc.hit_point, is_owner_identity_available = false})
            end
        end
        if candidates > 0 then return end
        check(scenario.Sleep(1000))
    until scenario.Time() >= deadline
    error("normal boss produced no living minion candidates before lethal scope")
end

function Battle:run()
    self.epochs, self.baseline = {}, {}
    for _, client in ipairs(self.clients) do
        local world = must(client.World:Ready())
        self.epochs[client.ID] = {game_id = world.game_id, zone_generation = world.zone_generation,
            peer_generation = world.peer_generation, transport_generation = world.transport_generation,
            hero_object_id = world.hero.object_id}
        must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    end
    local casterWorld = self:world(self.caster)
    arena = casterWorld.hero.is_movement_available and casterWorld.hero.movement_position or casterWorld.hero.position
    local initial = self:sample("empty_spawn_room_baseline")
    for _, npc in ipairs(initial.npcs or {}) do self.baseline[npc.object_id] = true end
    local observerPoint
    for _, radius in ipairs({30,25,20}) do
        for _, direction in ipairs({{-1,0},{0,-1},{0,1},{1,0},{-0.7071,-0.7071},{0.7071,0.7071}}) do
            local point = {x = arena.x+direction[1]*radius, y = arena.y+direction[2]*radius, z = arena.z}
            local path = must(self.observer.World:Path(arena, point))
            if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                observerPoint = path.goal.position
                break
            end
        end
        if observerPoint then break end
    end
    if not observerPoint then error("empty-room observer has no verified nearby mesh point") end
    self:place(self.observer, observerPoint)
    local receipt = must(self.caster.World:Spawn("ScaldronBoss.Noun"))
    self.bossID = receipt.object_id
    local boss = actor(self:world(self.caster), self.bossID)
    if not boss or string.lower(boss.noun) ~= stageOne then error("actual manual boss birth unavailable") end
    arena = boss.position
    log("boss_spawn_receipt", {object = receipt.object_id, request = receipt.request_id, noun = receipt.noun,
        game = receipt.game_id, peer = receipt.peer_generation, transport = receipt.transport_generation,
        zone = receipt.zone_generation, is_admitted = receipt.is_admitted, is_published = receipt.is_published,
        client = receipt.client_id, pid = receipt.pid, instance = receipt.instance_id,
        x = arena.x, y = arena.y, z = arena.z, mission = mission})
    check(scenario.Sleep(16000))
    self:waitRig(self.caster, true)
    self:sample("manual_boss_intro_complete")
    self:waitMinions("stage1_live_children", self.baseline)
    self:killBoss(1)
    self.stageOneAt = scenario.Time()
    local stageOneWorld = self:sample("stage1_lethal")
    local stageTwoBaseline = {}
    for _, npc in ipairs(stageOneWorld.npcs or {}) do stageTwoBaseline[npc.object_id] = true end
    self:clearPortals()
    self:waitStageTwo()
    self:waitMinions("stage2_live_children", stageTwoBaseline)
    self:killBoss(2)
    self:sample("stage2_lethal")
    for index = 1, 28 do check(scenario.Sleep(1000)); self:sample("final_death_" .. index) end
    local finalHPs = {}
    for _, client in ipairs(self.clients) do
        self:waitRig(client, false)
        local world = self:world(client)
        local native = must(client.World:SnapshotNative()).gameplay_state
        for _, member in ipairs(world.campaign.members) do
            for _, character in ipairs(member.characters) do
                if character.index == 0 then
                    if character.hit_point <= 0 then error("final hero is defeated") end
                    if finalHPs[member.user_id] and finalHPs[member.user_id] ~= character.hit_point then error("peer final hero HP differs") end
                    finalHPs[member.user_id] = character.hit_point
                    log("final_hero", {client = client.ID, user = member.user_id, hp = character.hit_point,
                        frame = native.frame_sequence, is_server_campaign_hp = true})
                end
            end
        end
        local capture, captureError = client.Diagnostics:Capture("scaldron-death-final")
        log("capture", {client = client.ID, manifest = capture and capture.manifest_path,
            error = captureError, boss = self.bossID})
    end
    log("capture_end", {boss = self.bossID, is_native_owner_teardown_audit_required = true})
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local battle = setmetatable({clients = {}, epochs = {}}, Battle)
    local profiles = {}
    for _, role in ipairs({"victim", "observer"}) do
        local profile = must(server:CreateProfile({name = "scd-" .. role,
            hero_nouns = {709733043, 3376462303, 185979955}, chain_progression = 1,
            equipped_parts = {
                {squad_category="pve", hero_index=0, rigblock_asset_id=265, level=100, rarity=0},
                {squad_category="pve", hero_index=0, rigblock_asset_id=515, level=100, rarity=0},
                {squad_category="pve", hero_index=1, rigblock_asset_id=322, level=100, rarity=0},
            }}))
        profiles[#profiles+1] = profile
        local client = must(profile:Launch({client_id = "scd-" .. role, launch_mode = "detached"}))
        battle.clients[#battle.clients+1] = client
        log("client", {role = role, client = client.ID, pid = client.PID,
            instance = client.InstanceID, user = profile.user_id})
        must(client:Login()); must(client.Scene:Wait(scene.Arsenal))
    end
    battle.caster, battle.observer = battle.clients[1], battle.clients[2]
    must(battle.caster.Party:Create("scaldron-death"))
    must(battle.caster.Party:Invite(battle.observer)); must(battle.observer.Party:Join(battle.caster))
    for _, client in ipairs(battle.clients) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    log("mission", {selection = mission, is_ambient_population_suppressed = true, boss_context = "ordinary_spawn_room"})
    must(battle.caster.Mission:Select(mission)); must(battle.caster.Mission:Start())
    must(battle.observer.Mission:WaitDeployment())
    check(scenario.Parallel({caster = function() must(battle.caster.Mission:Continue()) end,
        observer = function() must(battle.observer.Mission:Continue()) end}))
    local failure = scenario.Parallel({capture = function() battle:run() end})
    for _, client in ipairs(battle.clients) do check(client:Terminate()) end
    for _, profile in ipairs(profiles) do check(profile:Logout()) end
    check(failure)
end
