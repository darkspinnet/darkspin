---Build-103 Flak Cannon area/rank observations; all authored ranks are explicit SERVER fixtures.
---Reuses one owned 2v2 match; inputs with unknown outcomes stop.
local must,check = scenario.Must,scenario.Check
local sourceID,targetID = "pvp-slow-source","pvp-slow-primary"
local allyID,splashID = "pvp-slow-ally","pvp-slow-splash"
local cases = {
    {id="flak-alpha-rank1",noun=3524487169,name="SRS-42 Alpha",rank=1,duration=6000,is_movement=true},
    {id="flak-beta-rank2",noun=2445894411,name="SRS-42 Beta",rank=2,duration=4000},
    {id="flak-gamma-rank5",noun=3334429394,name="SRS-42 Gamma",rank=5,duration=6000,is_repeating=true},
    {id="flak-delta-rank6",noun=2038499613,name="SRS-42 Delta",rank=6,duration=4000,is_repeating=true},
}
local function pause(ms) check(scenario.Sleep(ms)) end
local function distance(first,second)
    local x,y=first.x-second.x,first.y-second.y
    return math.sqrt(x*x+y*y)
end
local function offset(point,x,y) return {x=point.x+x,y=point.y+y,z=point.z} end
local function jsonString(text)
    return '"'..tostring(text):gsub('[%z\1-\31\\"]',function(character)
        return string.format("\\u%04x",string.byte(character))
    end)..'"'
end
local function json(item)
    if item==nil then return "null" end
    if type(item)=="string" then return jsonString(item) end
    if type(item)=="number" or type(item)=="boolean" then return tostring(item) end
    if #item>0 then
        local entries={}
        for _,element in ipairs(item) do entries[#entries+1]=json(element) end
        return "["..table.concat(entries,",").."]"
    end
    local keys,entries={},{}
    for key in pairs(item) do keys[#keys+1]=key end
    table.sort(keys)
    for _,key in ipairs(keys) do entries[#entries+1]=jsonString(key)..":"..json(item[key]) end
    return "{"..table.concat(entries,",").."}"
end
local function record(event)
    event.suite,event.at_ms="pvp_flak",scenario.Time()
    local line=json(event)
    if #line>2048 then error("status log exceeds durable boundary") end
    check(scenario.Log(line))
end
local function read(client,pin)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if probe.pid~=client.PID or probe.instance_id~=client.InstanceID or not world.arena
        or not world.arena.is_combat_active or #world.arena.members~=4 or not world.hero
        or world.hero.hit_point<=0 or world.hero.hostile_movement_modifier==nil or world.hero.hostile_attack_modifier==nil or not native
        or not native.is_world_active or not native.position or not native.is_rig_ready then
        error("owned living native Arena/status producer unavailable")
    end
    if pin and (world.game_id~=pin.world.game_id or world.peer_generation~=pin.world.peer_generation
        or world.transport_generation~=pin.world.transport_generation or world.hero.object_id~=pin.world.hero.object_id
        or world.hero.index~=pin.world.hero.index or world.hero.noun~=pin.world.hero.noun
        or native.controlled_handle~=pin.native.controlled_handle) then error("status actor identity changed; no replay") end
    return {world=world,native=native,at_ms=scenario.Time()}
end
local function click(client,point)
    local receipt=must(client.World:Click(point))
    if not receipt.is_processed then error("native input unprocessed; no replay") end
end
local function holdMovement(client,point,duration,caseID,phase)
    local receipt=must(client.World:Hold(point,duration))
    local poll=receipt.gameplay_click
    record({event="movement_input",case_id=caseID,phase=phase,client_id=client.ID,pid=client.PID,
        goal=point,hold_ms=duration,is_processed=receipt.is_processed,
        is_projected=poll and poll.is_projected,
        is_press_poll_observed=poll and poll.is_press_poll_observed,
        is_release_poll_observed=poll and poll.is_release_poll_observed,
        is_native_ground_query_success=poll and poll.is_native_ground_query_success,
        chosen_ground_point=poll and poll.chosen_ground_point})
    if not receipt.is_processed or not poll or not poll.is_press_poll_observed or not poll.is_release_poll_observed then
        error("native movement lacks paired polls; no replay")
    end
end
local function reset(client)
    local resources=must(client.World:RestoreHero())
    local cooldowns=must(client.World:ResetCooldowns())
    if not resources.is_queued or resources.recipient_count~=4 or not cooldowns.is_cooldown_reset
        or cooldowns.recipient_count~=4 then error("fixture not published to both owned participants") end
end
local function ensureHero(client,noun)
    local world=must(client.World:SnapshotLiving())
    if world.hero.index~=0 then
        must(client.World:ResetCooldowns())
        must(client.World:SelectHero(0,20000))
        world=must(client.World:SnapshotLiving())
    end
    if world.hero.noun~=noun then
        local receipt=must(client.World:ReplaceHero(noun))
        if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count~=4 then
            error("replacement did not establish both native rigs")
        end
    end
    return read(client)
end
local function place(client,point,pin)
    local before=read(client,pin)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=4 then error("placement missing owned recipient") end
    local deadline=scenario.Time()+6000
    while scenario.Time()<deadline do
        local observed=read(client,pin)
        if observed.native.frame_sequence>before.native.frame_sequence and distance(observed.native.position,point)<.2 then return observed end
        pause(75)
    end
    error("native placement unobserved")
end
local function missing(failure)
    local cause=failure
    while cause and (not cause.Code or cause.Code=="") do cause=cause.Cause end
    return cause and cause.Code=="not_found"
end
local function launch(server,id,noun)
    local profile=must(server:CreateProfile({name=id,hero_nouns={noun,117394854,576185321},chain_progression=2,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=id,launch_mode="detached"}))
    must(client:Login());must(client.Scene:Wait(scene.Arsenal))
    return client
end
local function ownedPair(server)
    local source,sourceError=scenario.GetClient(sourceID)
    local target,targetError=scenario.GetClient(targetID)
    local ally,allyError=scenario.GetClient(allyID)
    local splash,splashError=scenario.GetClient(splashID)
    if source and target and ally and splash then
        local gameID=read(source).world.game_id
        for _,client in ipairs({target,ally,splash}) do
            if read(client).world.game_id~=gameID then error("owned clients are in different games") end
        end
        return source,target,ally,splash
    end
    if source or target or ally or splash or not missing(sourceError) or not missing(targetError)
        or not missing(allyError) or not missing(splashError) then error("partial owned2v2; no setup replay") end
    check(scenario.Parallel({
        source=function() source=launch(server,sourceID,cases[1].noun);return nil end,
        target=function() target=launch(server,targetID,700180862);return nil end,
        ally=function() ally=launch(server,allyID,700180862);return nil end,
        splash=function() splash=launch(server,splashID,700180862);return nil end,
    }))
    must(source.Party:Create("slow-one"));must(source.Party:Invite(ally));must(ally.Party:Join(source))
    must(target.Party:Create("slow-two"));must(target.Party:Invite(splash));must(splash.Party:Join(target))
    for _,client in ipairs({source,target,ally,splash}) do
        must(client.Scene:Set(scene.Navigation));must(client:WaitForReportedLocation("navigation",15000))
    end
    must(server:StartPVP({teams={{source,ally},{target,splash}}}))
    check(scenario.Parallel({
        source=function() must(source.PVP:Accept());return nil end,
        target=function() must(target.PVP:Accept());return nil end,
        ally=function() must(ally.PVP:Accept());return nil end,
        splash=function() must(splash.PVP:Accept());return nil end,
    }))
    check(scenario.Parallel({
        source=function() must(source.PVP:Wait());return nil end,
        target=function() must(target.PVP:Wait());return nil end,
        ally=function() must(ally.PVP:Wait());return nil end,
        splash=function() must(splash.PVP:Wait());return nil end,
    }))
    return source,target,ally,splash
end
local function serverPosition(observed)
    if not observed.world.hero.is_movement_available or not observed.world.hero.movement_position then
        error("actual sampled server motion unavailable")
    end
    return observed.world.hero.movement_position
end
local function isSlow(observed)
    return math.abs(observed.world.hero.hostile_movement_modifier+.5)<.001
        and math.abs(observed.world.hero.hostile_attack_modifier)<.001
end
local function isNormal(observed)
    return math.abs(observed.world.hero.hostile_movement_modifier)<.001
        and math.abs(observed.world.hero.hostile_attack_modifier)<.001
end
local function casterIsolated(source,center)
    local observed=read(source)
    if not isNormal(observed) or distance(observed.native.position,center)>.25 then
        error("Flak Cannon caster moved or received hostile slow")
    end
end
local function allyIsolated(ally,pin,before)
    local observed=read(ally,pin)
    if not isNormal(observed) or observed.world.hero.hit_point~=before.world.hero.hit_point
        or distance(observed.native.position,before.native.position)>.25 then
        error("nearby friendly received damage, slow or movement")
    end
end
local function measure(case,target,pin,phase,isExpectedSlow)
    local before=read(target,pin)
    if isExpectedSlow and not isSlow(before) or not isExpectedSlow and not isNormal(before) then
        error("movement initial modifier differs: "..phase)
    end
    holdMovement(target,offset(before.native.position,0,6),650,case.id,phase)
    local after=read(target,pin)
    if after.native.frame_sequence<=before.native.frame_sequence
        or isExpectedSlow and not isSlow(after) or not isExpectedSlow and not isNormal(after) then
        error("movement frame or modifier changed: "..phase)
    end
    local elapsed=after.at_ms-before.at_ms
    local result={native_speed=distance(before.native.position,after.native.position)/(elapsed/1000),
        server_speed=distance(serverPosition(before),serverPosition(after))/(elapsed/1000)}
    record({event="movement_measurement",case_id=case.id,phase=phase,elapsed_ms=elapsed,hold_ms=650,
        native_before=before.native.position,native_after=after.native.position,
        server_before=serverPosition(before),server_after=serverPosition(after),
        native_speed=result.native_speed,server_speed=result.server_speed,
        hostile_movement_modifier=after.world.hero.hostile_movement_modifier,
        hostile_attack_modifier=after.world.hero.hostile_attack_modifier})
    if result.native_speed<.2 or result.server_speed<.2 then error("movement blocked: "..phase) end
    return result
end
local function ratio(case,phase,measured,baseline,minimum,maximum)
    local nativeRatio=measured.native_speed/baseline.native_speed
    local serverRatio=measured.server_speed/baseline.server_speed
    record({event="movement_ratio",case_id=case.id,phase=phase,native_ratio=nativeRatio,
        server_ratio=serverRatio,minimum=minimum,maximum=maximum})
    if nativeRatio<minimum or nativeRatio>maximum or serverRatio<minimum or serverRatio>maximum then
        error("native/server slow ratio outside observed contract: "..phase)
    end
end
local function fixtureRank(client,pin,rank)
    read(client,pin)
    local receipt=must(client.World:SetAbilityRank(rank))
    if not receipt.is_ability_rank_set or not receipt.is_server_rank_fixture or receipt.is_queued
        or receipt.ability_rank~=rank or receipt.object_id~=pin.world.hero.object_id or receipt.recipient_count~=4 then
        error("server rank fixture not established")
    end
    record({event="server_rank_fixture",rank=rank,client_id=client.ID,pid=client.PID,
        source_object=pin.world.hero.object_id,is_native_rank_selection=false})
end
local function runCase(case,source,target,ally,splash)
    local sourcePin=ensureHero(source,case.noun)
    local targetPin,allyPin,splashPin=read(target),read(ally),read(splash)
    click(source,sourcePin.native.position);click(target,targetPin.native.position)
    for _,client in ipairs({source,target,ally,splash}) do reset(client) end
    local center=read(source,sourcePin).native.position
    local pose=offset(center,5,0)
    place(target,pose,targetPin)
    local baseline
    if case.is_movement then
        baseline=measure(case,target,targetPin,"baseline",false)
        place(target,pose,targetPin)
    end
    place(ally,offset(center,7,0),allyPin);place(splash,offset(center,5,-2),splashPin)
    fixtureRank(source,sourcePin,case.rank)
    local beforeSource,beforeTarget,beforeAlly,beforeSplash=read(source,sourcePin),read(target,targetPin),read(ally,allyPin),read(splash,splashPin)
    record({event="case_start",case_id=case.id,hero_name=case.name,noun=case.noun,asset="MissileTempestSupport",
        ability_name="Flak Cannon",server_fixture_rank=case.rank,is_native_rank_selection=false,
        source_id=source.ID,source_pid=source.PID,source_instance=source.InstanceID,
        source_object=sourcePin.world.hero.object_id,source_peer_generation=sourcePin.world.peer_generation,
        source_transport_generation=sourcePin.world.transport_generation,native_ability_ranks=sourcePin.native.ability_ranks,
        target_id=target.ID,target_pid=target.PID,target_object=targetPin.world.hero.object_id,
        splash_id=splash.ID,splash_pid=splash.PID,splash_object=splashPin.world.hero.object_id,
        ally_id=ally.ID,ally_pid=ally.PID,ally_object=allyPin.world.hero.object_id,
        target_position=beforeTarget.native.position,splash_position=beforeSplash.native.position,
        duration_ms=case.duration,is_repeating=case.is_repeating or false})
    local started=scenario.Time()
    local button=must(source.UI:Click(640,655))
    if not button.is_processed then error("native Flak Cannon hotbar unprocessed; no replay") end
    pause(50)
    local aim=beforeTarget.native.position
    click(source,{x=aim.x,y=aim.y,z=aim.z+1.5})
    local deadline=scenario.Time()+3000
    local accepted
    while scenario.Time()<deadline do
        local first,second,caster=read(target,targetPin),read(splash,splashPin),read(source,sourcePin)
        casterIsolated(source,center);allyIsolated(ally,allyPin,beforeAlly)
        if isSlow(first) and isSlow(second) and caster.world.hero.mana_point<beforeSource.world.hero.mana_point then accepted=first;break end
        pause(25)
    end
    if not accepted then error("Flak Cannon power/both-victim slow unobserved; no replay") end
    local slowed
    if case.is_movement then
        slowed=measure(case,target,targetPin,"slowed",true)
        ratio(case,"slowed",slowed,baseline,.3,.7)
    end
    local windowEnd=started+(case.is_repeating and 2100 or 550)
    while scenario.Time()<windowEnd do
        local first,second=read(target,targetPin),read(splash,splashPin)
        casterIsolated(source,center);allyIsolated(ally,allyPin,beforeAlly)
        record({event="pulse_sample",case_id=case.id,elapsed_ms=scenario.Time()-started,
            target_hp=first.world.hero.hit_point,splash_hp=second.world.hero.hit_point,
            target_slow=first.world.hero.hostile_movement_modifier,splash_slow=second.world.hero.hostile_movement_modifier,
            is_pulse_packet_count_verified=false})
        pause(100)
    end
    local afterSource,afterTarget,afterSplash=read(source,sourcePin),read(target,targetPin),read(splash,splashPin)
    if afterSource.world.hero.hit_point~=beforeSource.world.hero.hit_point then error("Flak Cannon damaged its caster") end
    local damage=beforeTarget.world.hero.hit_point-afterTarget.world.hero.hit_point
    local splashDamage=beforeSplash.world.hero.hit_point-afterSplash.world.hero.hit_point
    local isBothPositive=damage>0 and splashDamage>0
    record({event="cast_result",case_id=case.id,server_fixture_rank=case.rank,
        status=isBothPositive and "damage_and_slow_observed" or "requires_trace_for_zero_damage",
        primary_role_damage=damage,splash_role_damage=splashDamage,is_both_victim_damage_positive=isBothPositive,
        power_before=beforeSource.world.hero.mana_point,power_after=afterSource.world.hero.mana_point,
        power_spent=beforeSource.world.hero.mana_point-afterSource.world.hero.mana_point,
        is_defense_confirmed=false,is_pulse_packet_count_verified=false,is_native_rank_selection=false})
    deadline=accepted.at_ms+case.duration+2400
    local released
    while scenario.Time()<deadline do
        local first,second=read(target,targetPin),read(splash,splashPin)
        casterIsolated(source,center);allyIsolated(ally,allyPin,beforeAlly)
        if isNormal(first) and isNormal(second) then released=first;break end
        pause(50)
    end
    if not released or released.at_ms-accepted.at_ms<case.duration-350 then error("Flak Cannon slow expiry unobserved or early") end
    if case.is_movement then
        place(target,pose,targetPin)
        local recovered=measure(case,target,targetPin,"after-expiry",false)
        ratio(case,"after-expiry",recovered,baseline,.75,1.3)
    end
    record({event="case_end",case_id=case.id,status=isBothPositive and "passed" or "requires_trace_for_zero_damage",
        observed_slow_after_first_ms=released.at_ms-accepted.at_ms,is_friendly_isolation_verified=true,
        is_native_rank_selection=false,is_repeating_pulse_timing_verified=false})
    return isBothPositive
end
return function()
    check(scenario.RequireAPI(1))
    local source,target,ally,splash=ownedPair(must(scenario.StartServer({})))
    local clients={source,target,ally,splash}
    local pids,instances={},{},{}
    for _,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Flak Cannon hotbar requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned native identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
    end
    ensureHero(target,700180862);ensureHero(ally,700180862);ensureHero(splash,700180862)
    local passed=0
    for _,case in ipairs(cases) do if runCase(case,source,target,ally,splash) then passed=passed+1 end end
    fixtureRank(source,read(source),0)
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=read(client,befores[client.ID])
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
        record({event="final_actor",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
            object_id=final.world.hero.object_id,noun=final.world.hero.noun,game_id=final.world.game_id,
            peer_generation=final.world.peer_generation,transport_generation=final.world.transport_generation,
            native_frame=final.native.frame_sequence,native_ability_ranks=final.native.ability_ranks})
        local capture=must(client.Diagnostics:Capture("flak-area-final"))
        record({event="diagnostic",client_id=client.ID,manifest=capture.manifest_path})
        must(client.UI:Screenshot("flak-final-"..client.ID))
    end
    record({event="suite_end",passed=passed,completed=4,is_four_clients_live=true,
        is_server_fixture_rank_coverage=true,is_native_rank_selection_verified=false,
        is_projectile_destruction_verified=false,is_flak_buff_verified=false,is_all_rank_parity_verified=false})
end
