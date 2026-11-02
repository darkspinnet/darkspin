---Native Delaying Sphere entry/exit/expiry and movement observations.
---Reuse the explicitly owned bulk duel when present; otherwise create a fresh duel.
---Projectile captures require post-run receiving-trace analysis; they are not a Lua parity verdict.
local must, check = scenario.Must, scenario.Check
local sourceID, targetID = "pvp-semantics-source", "pvp-semantics-target"
local sourceNoun, targetNoun = 3618328019, 700180862
local radius = 8 -- Build-103 SpacetimeRandom2 production contract.
local sampleMS = 650

local function pause(ms) check(scenario.Sleep(ms)) end

local function jsonString(text)
    return '"' .. tostring(text):gsub('[%z\1-\31\\"]', function(character)
        return string.format("\\u%04x", string.byte(character))
    end) .. '"'
end

local function json(item)
    if item == nil then return "null" end
    if type(item) == "string" then return jsonString(item) end
    if type(item) == "number" then return tostring(item) end
    if type(item) == "boolean" then return item and "true" or "false" end
    local keys, entries = {}, {}
    for key in pairs(item) do keys[#keys+1] = key end
    table.sort(keys)
    for _, key in ipairs(keys) do entries[#entries+1] = jsonString(key) .. ":" .. json(item[key]) end
    return "{" .. table.concat(entries, ",") .. "}"
end

local function record(event)
    event.suite, event.at_ms = "pvp_sphere", scenario.Time()
    local line = json(event)
    if #line > 2048 then error("sphere observation exceeds log boundary") end
    check(scenario.Log(line))
end

local function distance(first, second)
    local x, y = first.x-second.x, first.y-second.y
    return math.sqrt(x*x+y*y)
end

local function offset(center, x, y)
    return {x=center.x+x, y=center.y+y, z=center.z}
end

local function read(client, pin)
    local world = must(client.World:SnapshotLiving())
    local native = must(client.World:SnapshotNative()).gameplay_state
    if not world.arena or not world.arena.is_combat_active or #world.arena.members ~= 2
        or not native or not native.is_available or not native.is_world_active or not native.position
        or not native.is_rig_observed or not native.is_rig_ready or not native.controlled_handle then
        error("owned active Arena and native rig observation required")
    end
    if world.hero.is_movement_available == nil or world.hero.movement_speed == nil or world.hero.hostile_movement_modifier == nil
        or world.hero.hostile_attack_modifier == nil or world.hero.sphere_membership_count == nil then
        error("read-only sphere and sampled motion producer unavailable")
    end
    if pin and (world.game_id ~= pin.game_id or world.peer_generation ~= pin.peer_generation
        or world.transport_generation ~= pin.transport_generation or world.hero.object_id ~= pin.object_id
        or world.hero.index ~= pin.index or world.hero.noun ~= pin.noun
        or native.controlled_handle ~= pin.handle or native.owner_thread_id ~= pin.owner_thread) then
        error("sphere operation crossed a pinned actor or realm; no replay")
    end
    return {world=world, native=native, at_ms=scenario.Time()}
end

local function movementPosition(observed)
    if not observed.world.hero.is_movement_available or not observed.world.hero.movement_position then
        error("sampled server movement unavailable: "..tostring(observed.world.hero.movement_unavailable_reason))
    end
    return observed.world.hero.movement_position
end

local function actorPin(observed)
    local world, native = observed.world, observed.native
    return {game_id=world.game_id, peer_generation=world.peer_generation,
        transport_generation=world.transport_generation, object_id=world.hero.object_id,
        index=world.hero.index, noun=world.hero.noun, handle=native.controlled_handle,
        owner_thread=native.owner_thread_id}
end

local function logSample(client, phase, observed)
    local hero, native = observed.world.hero, observed.native
    record({event="motion_sample", phase=phase, client_id=client.ID, pid=client.PID,
        instance_id=client.InstanceID, game_id=observed.world.game_id, object_id=hero.object_id,
        native_handle=native.controlled_handle, native_frame=native.frame_sequence,
        sample_at_ms=observed.at_ms, native_position=native.position,
        server_position=movementPosition(observed), server_speed=hero.movement_speed,
        sphere_membership_count=hero.sphere_membership_count,
        hostile_movement_modifier=hero.hostile_movement_modifier,
        hostile_attack_modifier=hero.hostile_attack_modifier})
end

local function stop(client, pin)
    local observed = read(client,pin)
    local receipt = must(client.World:Click(observed.native.position))
    if not receipt.is_processed then error("native stop edge unprocessed; no replay") end
end

local function reset(client)
    local resources = must(client.World:RestoreHero())
    local cooldowns = must(client.World:ResetCooldowns())
    if not resources.is_queued or resources.recipient_count ~= 2
        or not cooldowns.is_cooldown_reset or cooldowns.recipient_count ~= 2 then
        error("owned fixture did not queue to both duel participants")
    end
end

local function ensureHero(client, noun, isForced)
    local world = must(client.World:SnapshotLiving())
    if world.hero.index ~= 0 then
        must(client.World:ResetCooldowns())
        must(client.World:SelectHero(0,20000))
        world = must(client.World:SnapshotLiving())
    end
    if world.hero.noun ~= noun or isForced then
        local receipt = must(client.World:ReplaceHero(noun))
        if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count ~= 2 then
            error("replacement did not establish native rig readiness")
        end
    end
    return read(client)
end

local function place(client, point, pin)
    local before = read(client,pin)
    local receipt = must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count ~= 2 then error("owned placement not published to both clients") end
    local deadline = scenario.Time()+5000
    while scenario.Time()<deadline do
        local observed = read(client,pin)
        if distance(observed.native.position,point)<0.2 and distance(movementPosition(observed),point)<0.2
            and observed.native.frame_sequence>before.native.frame_sequence then return observed end
        pause(75)
    end
    error("native and sampled server placement did not converge")
end

local function assertCaster(pair, phase)
    local source = read(pair.source,pair.sourcePin)
    local hero = source.world.hero
    local serverPosition = hero.is_movement_available and hero.movement_position or hero.position
    if hero.sphere_membership_count ~= 0 or math.abs(hero.hostile_movement_modifier)>0.001
        or math.abs(hero.hostile_attack_modifier)>0.001
        or distance(source.native.position,pair.center)>0.25
        or distance(serverPosition,pair.center)>0.25 then
        error("caster moved or received its own sphere slow during "..phase)
    end
    return source
end

local function isMembership(observed, isInside)
    local hero = observed.world.hero
    if isInside then
        return hero.sphere_membership_count>0 and math.abs(hero.hostile_movement_modifier+0.6)<0.001
            and math.abs(hero.hostile_attack_modifier+0.4)<0.001
    end
    return hero.sphere_membership_count==0 and math.abs(hero.hostile_movement_modifier)<0.001
        and math.abs(hero.hostile_attack_modifier)<0.001
end

local function waitMembership(pair, isInside, phase, timeout)
    local deadline = scenario.Time()+timeout
    while scenario.Time()<deadline do
        local observed = read(pair.target,pair.targetPin)
        assertCaster(pair,phase)
        if isMembership(observed,isInside) then
            logSample(pair.target,phase,observed)
            record({event="membership",phase=phase,is_inside=isInside,
                distance_native=distance(observed.native.position,pair.center),
                distance_server=distance(movementPosition(observed),pair.center)})
            return observed
        end
        pause(75)
    end
    error("sphere membership deadline: "..phase.."; no input replay")
end

local function hold(client, point, duration)
    local receipt = must(client.World:Hold(point,duration))
    local poll = receipt.gameplay_click
    record({event="movement_input",client_id=client.ID,pid=client.PID,goal=point,
        is_processed=receipt.is_processed,is_projected=poll and poll.is_projected,
        is_press_poll_observed=poll and poll.is_press_poll_observed,
        is_release_poll_observed=poll and poll.is_release_poll_observed,
        is_native_ground_query_success=poll and poll.is_native_ground_query_success,
        chosen_ground_point=poll and poll.chosen_ground_point})
    if not receipt.is_processed or not poll or not poll.is_press_poll_observed or not poll.is_release_poll_observed then
        error("native movement lacks paired press/release polls; no replay")
    end
end

local function measure(pair, phase, goal, isInside)
    local first = read(pair.target,pair.targetPin)
    if not isMembership(first,isInside) then error("measurement initial membership differs: "..phase) end
    logSample(pair.target,phase.."-start",first)
    hold(pair.target,goal,sampleMS)
    local last = read(pair.target,pair.targetPin)
    assertCaster(pair,phase)
    if not isMembership(last,isInside) then error("measurement crossed the requested membership: "..phase) end
    if last.native.frame_sequence<=first.native.frame_sequence then error("native frame did not advance: "..phase) end
    logSample(pair.target,phase.."-end",last)
    local seconds = (last.at_ms-first.at_ms)/1000
    local nativeSpeed = distance(last.native.position,first.native.position)/seconds
    local serverSpeed = distance(movementPosition(last),movementPosition(first))/seconds
    if nativeSpeed<0.1 or serverSpeed<0.1 then error("movement sample blocked or stationary: "..phase) end
    local result={native_speed=nativeSpeed,server_speed=serverSpeed,
        retained_speed=last.world.hero.movement_speed,elapsed_ms=last.at_ms-first.at_ms}
    record({event="movement_measurement",phase=phase,native_speed=nativeSpeed,server_speed=serverSpeed,
        retained_speed=result.retained_speed,elapsed_ms=result.elapsed_ms,hold_ms=sampleMS})
    return result
end

local function travelMembership(pair, goal, isInside, phase, timeout)
    local deadline=scenario.Time()+timeout
    while scenario.Time()<deadline do
        hold(pair.target,goal,sampleMS)
        local observed=read(pair.target,pair.targetPin)
        assertCaster(pair,phase)
        logSample(pair.target,phase.."-held-travel",observed)
        if isMembership(observed,isInside) then
            record({event="membership",phase=phase,is_inside=isInside,
                distance_native=distance(observed.native.position,pair.center),
                distance_server=distance(movementPosition(observed),pair.center)})
            return observed
        end
    end
    error("held travel membership deadline: "..phase.."; paired inputs completed without requested boundary")
end

local function checkRatio(phase, measured, baseline, minimum, maximum)
    local nativeRatio,serverRatio=measured.native_speed/baseline.native_speed,measured.server_speed/baseline.server_speed
    record({event="movement_ratio",phase=phase,native_ratio=nativeRatio,server_ratio=serverRatio,
        minimum=minimum,maximum=maximum,is_native_displacement_measured=true,is_server_displacement_measured=true})
    if nativeRatio<minimum or nativeRatio>maximum or serverRatio<minimum or serverRatio>maximum then
        error("native/server speed ratio outside observed contract: "..phase)
    end
end

local function castAt(client, x, point, asset)
    local button = must(client.UI:Click(x,655))
    if not button.is_processed then error("native ability hotbar edge unprocessed; no replay") end
    pause(150)
    local receipt = must(client.World:Click(point))
    if not receipt.is_processed then error("native ability aim edge unprocessed; no replay") end
    record({event="native_cast_processed",client_id=client.ID,pid=client.PID,asset=asset,
        is_native_ability_identity_verified=false,is_packet_parity_verified=false})
end

local function castSphere(pair, phase)
    local before = assertCaster(pair,phase)
    castAt(pair.source,605,before.native.position,"SpacetimeRandom2")
    local deadline=scenario.Time()+4000
    while scenario.Time()<deadline do
        local observed=assertCaster(pair,phase)
        if observed.world.hero.mana_point<before.world.hero.mana_point-1 then
            record({event="sphere_resource_admission",phase=phase,source_id=pair.source.ID,
                power_before=before.world.hero.mana_point,power_after=observed.world.hero.mana_point,
                radius=radius,duration_ms=12000})
            return
        end
        pause(75)
    end
    error("sphere resource admission unobserved; no replay")
end

local function capture(pair, phase)
    for _,client in ipairs({pair.source,pair.target}) do
        local receipt=must(client.Diagnostics:Capture("sphere-"..phase))
        record({event="diagnostic",phase=phase,client_id=client.ID,pid=client.PID,manifest=receipt.manifest_path})
    end
end

local function movementCase(pair)
    local start,goal=offset(pair.center,-5,3),offset(pair.center,6,3)
    place(pair.target,start,pair.targetPin)
    local baseline=measure(pair,"baseline",goal,false)
    place(pair.target,start,pair.targetPin)
    castSphere(pair,"movement")
    waitMembership(pair,true,"initial-entry",4000)
    local inside=measure(pair,"inside",goal,true)
    checkRatio("inside",inside,baseline,0.25,0.60)
    capture(pair,"inside")
    local exited=travelMembership(pair,offset(pair.center,-14,3),false,"native-exit",6000)
    -- Continue tangentially from the observed boundary; the farther negative-X
    -- goal is clipped by an arena obstacle and cannot measure ordinary speed.
    local outside=measure(pair,"outside",offset(exited.native.position,0,6),false)
    checkRatio("outside",outside,baseline,0.72,1.30)
    travelMembership(pair,start,true,"native-reentry",6000)
    stop(pair.target,pair.targetPin)
    capture(pair,"reentry")
    waitMembership(pair,false,"expiry",15000)
    local expired=measure(pair,"after-expiry",goal,false)
    checkRatio("after-expiry",expired,baseline,0.72,1.30)
    capture(pair,"expired")
    record({event="movement_case_complete",is_membership_entry_exit_expiry_verified=true,
        is_native_and_server_movement_measured=true,is_attack_rate_measured=false})
end

local function projectileWindow(pair, phase)
    record({event="projectile_window_start",phase=phase,asset="PlasmaRandom_FireWave",
        source_id=pair.target.ID,source_pid=pair.target.PID,caster_id=pair.source.ID,
        center=pair.center,origin=offset(pair.center,-9.5,3),aim=offset(pair.center,10.5,3)})
    castAt(pair.target,605,offset(pair.center,10.5,3),"PlasmaRandom_FireWave")
    local deadline=scenario.Time()+4500
    while scenario.Time()<deadline do
        pause(100)
        local target=read(pair.target,pair.targetPin)
        assertCaster(pair,phase)
        if not isMembership(target,false) then error("projectile shooter entered the stationary sphere") end
    end
    capture(pair,phase)
    record({event="projectile_window_end",phase=phase,is_projectile_dilation_verified=false,
        verification="receiving-trace post-analysis required; snapshots do not expose projectile motion"})
end

local function projectileCase(pair)
    place(pair.target,offset(pair.center,-9.5,3),pair.targetPin)
    reset(pair.target)
    projectileWindow(pair,"projectile-baseline")
    reset(pair.source)
    reset(pair.target)
    castSphere(pair,"projectile")
    pause(400) -- The following window is evidence collection, not a live projectile verdict.
    projectileWindow(pair,"projectile-sphere")
    pause(8500)
    waitMembership(pair,false,"projectile-final-outside",2000)
end

local function isMissing(failure)
    local cause=failure
    while cause and (cause.Code==nil or cause.Code=="") do cause=cause.Cause end
    return cause and cause.Code=="not_found"
end

local function getOwnedPair(server)
    local source,sourceError=scenario.GetClient(sourceID)
    local target,targetError=scenario.GetClient(targetID)
    if source and target then
        local first,second=must(source.World:SnapshotLiving()),must(target.World:SnapshotLiving())
        if first.game_id~=second.game_id then error("explicitly owned reused clients are in different realms") end
        return {source=source,target=target,isReused=true}
    end
    if source or target or not isMissing(sourceError) or not isMissing(targetError) then
        error("partial or unavailable owned pair cannot be reused or replayed")
    end
    local sourceProfile=must(server:CreateProfile({name=sourceID,hero_nouns={sourceNoun,117394854,3068979135},chain_progression=2,is_pvp_enabled=true}))
    local targetProfile=must(server:CreateProfile({name=targetID,hero_nouns={targetNoun,117394854,3068979135},chain_progression=2,is_pvp_enabled=true}))
    source=must(sourceProfile:Launch({client_id=sourceID,launch_mode="detached"}))
    target=must(targetProfile:Launch({client_id=targetID,launch_mode="detached"}))
    must(source:Login());must(target:Login())
    must(source.Scene:Wait(scene.Arsenal));must(target.Scene:Wait(scene.Arsenal))
    must(source.Party:Create("sphere"));must(source.Party:Invite(target));must(target.Party:Join(source))
    must(source.Scene:Set(scene.Navigation));must(target.Scene:Set(scene.Navigation))
    must(source:WaitForReportedLocation("navigation",15000));must(target:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{target}}}))
    check(scenario.Parallel({source=function()must(source.PVP:Accept());return nil end,
        target=function()must(target.PVP:Accept());return nil end}))
    must(source.PVP:Wait());must(target.PVP:Wait())
    return {source=source,target=target,isReused=false}
end

return function()
    check(scenario.RequireAPI(1))
    local pair=getOwnedPair(must(scenario.StartServer({})))
    for _,client in ipairs({pair.source,pair.target}) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then
            error("native sphere hotbar coordinates require both owned 1280x720 windows")
        end
    end
    if pair.source.PID==pair.target.PID or pair.source.InstanceID==pair.target.InstanceID then
        error("sphere requires two independently owned native clients")
    end
    -- A reused duel may retain prior statuses and casts. The owned fixture
    -- retires those producers before recording the new actor lifetime.
    ensureHero(pair.source,sourceNoun,pair.isReused);ensureHero(pair.target,targetNoun,pair.isReused)
    pair.sourcePin=actorPin(read(pair.source));pair.targetPin=actorPin(read(pair.target))
    stop(pair.source,pair.sourcePin);stop(pair.target,pair.targetPin)
    reset(pair.source);reset(pair.target)
    pair.center=read(pair.source,pair.sourcePin).native.position
    record({event="pair",game_id=pair.sourcePin.game_id,source_id=pair.source.ID,source_pid=pair.source.PID,
        source_instance=pair.source.InstanceID,target_id=pair.target.ID,target_pid=pair.target.PID,
        target_instance=pair.target.InstanceID,center=pair.center})
    movementCase(pair)
    projectileCase(pair)
    local sourceBefore,targetBefore=read(pair.source,pair.sourcePin),read(pair.target,pair.targetPin)
    pause(100)
    local sourceAfter,targetAfter=read(pair.source,pair.sourcePin),read(pair.target,pair.targetPin)
    if sourceAfter.native.frame_sequence<=sourceBefore.native.frame_sequence
        or targetAfter.native.frame_sequence<=targetBefore.native.frame_sequence
        or sourceAfter.world.game_id~=targetAfter.world.game_id then error("final independent native liveness unavailable") end
    capture(pair,"final")
    record({event="suite_end",is_movement_case_verified=true,is_both_clients_live=true,
        is_projectile_dilation_verified=false,is_full_ability_parity_verified=false,
        remaining="projectile receiving-trace analysis and native attack-rate measurement"})
end
