---Build-103 Dazing Shot accepted-hit slow on four installed Titan variants.
---Reuses one owned 2v2 match; inputs with unknown outcomes stop.
local must,check = scenario.Must,scenario.Check
local sourceID,targetID = "pvp-slow-source","pvp-slow-primary"
local allyID,splashID = "pvp-slow-ally","pvp-slow-splash"
local cases = {
    {id="dazing-titan-alpha",noun=3068979135,name="Titan Alpha"},
    {id="dazing-titan-beta",noun=1194511925,name="Titan Beta"},
    {id="dazing-titan-gamma",noun=1341857832,name="Titan Gamma"},
    {id="dazing-titan-delta",noun=3380544995,name="Titan Delta"},
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
    local keys,entries={},{}
    for key in pairs(item) do keys[#keys+1]=key end
    table.sort(keys)
    for _,key in ipairs(keys) do entries[#entries+1]=jsonString(key)..":"..json(item[key]) end
    return "{"..table.concat(entries,",").."}"
end
local function record(event)
    event.suite,event.at_ms="pvp_slow",scenario.Time()
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
        error("Dazing Shot caster moved or received hostile slow")
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
local function runCase(case,source,target,ally,splash)
    local sourcePin=ensureHero(source,case.noun)
    local targetPin,allyPin,splashPin=read(target),read(ally),read(splash)
    click(source,sourcePin.native.position);click(target,targetPin.native.position)
    for _,client in ipairs({source,target,ally,splash}) do reset(client) end
    local center=read(source,sourcePin).native.position
    local pose=offset(center,5,0)
    place(target,pose,targetPin)
    local started=scenario.Time()
    record({event="case_start",case_id=case.id,hero_name=case.name,noun=case.noun,
        asset="TCShieldedSentinelSupport",ability_name="Dazing Shot",game_id=sourcePin.world.game_id,
        source_id=source.ID,source_pid=source.PID,source_instance=source.InstanceID,
        source_object=sourcePin.world.hero.object_id,source_noun=sourcePin.world.hero.noun,
        target_id=target.ID,target_pid=target.PID,target_instance=target.InstanceID,
        target_object=targetPin.world.hero.object_id,target_noun=targetPin.world.hero.noun,
        source_peer_generation=sourcePin.world.peer_generation,target_peer_generation=targetPin.world.peer_generation,
        source_transport_generation=sourcePin.world.transport_generation,target_transport_generation=targetPin.world.transport_generation})
    local baseline=measure(case,target,targetPin,"baseline",false)
    place(target,pose,targetPin)
    place(ally,offset(center,7,0),allyPin)
    place(splash,offset(center,5,-2),splashPin)
    local beforeAlly,beforeSplash=read(ally,allyPin),read(splash,splashPin)
    local beforeSource,beforeTarget=read(source,sourcePin),read(target,targetPin)
    record({event="secondary_identity",case_id=case.id,ally_id=ally.ID,ally_pid=ally.PID,
        ally_object=allyPin.world.hero.object_id,splash_id=splash.ID,splash_pid=splash.PID,
        splash_object=splashPin.world.hero.object_id,ally_position=beforeAlly.native.position,
        splash_position=beforeSplash.native.position,target_position=beforeTarget.native.position})
    local button=must(source.UI:Click(640,655))
    if not button.is_processed then error("native support hotbar edge unprocessed; no replay") end
    pause(150)
    local aim=offset(beforeTarget.native.position,0,0)
    aim.z=aim.z+1.5
    click(source,aim)
    record({event="native_cast_processed",case_id=case.id,asset="TCShieldedSentinelSupport",
        aim=aim,is_native_ability_identity_verified=false,is_packet_parity_verified=false})
    local accepted,deadline=nil,scenario.Time()+5000
    while scenario.Time()<deadline do
        local observed=read(target,targetPin)
        local secondary=read(splash,splashPin)
        casterIsolated(source,center)
        allyIsolated(ally,allyPin,beforeAlly)
        if isSlow(observed) and observed.world.hero.hit_point<beforeTarget.world.hero.hit_point
            and isSlow(secondary) and secondary.world.hero.hit_point<beforeSplash.world.hero.hit_point then
            accepted=observed;break
        end
        pause(50)
    end
    if not accepted then error("Dazing Shot accepted damage and slow unobserved; no replay") end
    local afterSource,afterSplash=read(source,sourcePin),read(splash,splashPin)
    if afterSource.world.hero.hit_point~=beforeSource.world.hero.hit_point then error("Dazing Shot damaged its caster") end
    record({event="slow_observed",case_id=case.id,damage=beforeTarget.world.hero.hit_point-accepted.world.hero.hit_point,
        splash_damage=beforeSplash.world.hero.hit_point-afterSplash.world.hero.hit_point,
        power_before=beforeSource.world.hero.mana_point,power_after=afterSource.world.hero.mana_point,
        target_frame=accepted.native.frame_sequence,hostile_movement_modifier=accepted.world.hero.hostile_movement_modifier})
    local slowed=measure(case,target,targetPin,"slowed",true)
    ratio(case,"slowed",slowed,baseline,.3,.7)
    casterIsolated(source,center)
    if case.id=="dazing-titan-alpha" then
        must(source.World:ResetCooldowns())
        must(source.World:SelectHero(1,20000))
        local afterSwap=read(target,targetPin)
        if afterSwap.at_ms-accepted.at_ms>=4500 or not isSlow(afterSwap) or not isSlow(read(splash,splashPin)) then
            error("caster swap did not retain target-owned slow before expiry")
        end
        record({event="caster_swap",case_id=case.id,is_target_slowed=true,
            elapsed_since_slow_ms=afterSwap.at_ms-accepted.at_ms})
    end
    for _,client in ipairs({source,target}) do
        local capture=must(client.Diagnostics:Capture(case.id.."-slow"))
        record({event="diagnostic",case_id=case.id,client_id=client.ID,pid=client.PID,manifest=capture.manifest_path})
    end
    deadline=accepted.at_ms+6500
    local released
    while scenario.Time()<deadline do
        local observed=read(target,targetPin)
        casterIsolated(source,center)
        allyIsolated(ally,allyPin,beforeAlly)
        if isNormal(observed) and isNormal(read(splash,splashPin)) then released=observed;break end
        pause(60)
    end
    if not released then error("target-owned slow did not expire") end
    -- Reuse the baseline corridor so map obstacles cannot turn restored speed
    -- into a different route-length measurement after the slowed displacement.
    place(target,pose,targetPin)
    local recovered=measure(case,target,targetPin,"after-expiry",false)
    ratio(case,"after-expiry",recovered,baseline,.75,1.3)
    casterIsolated(source,center)
    allyIsolated(ally,allyPin,beforeAlly)
    record({event="case_end",case_id=case.id,status="passed",elapsed_ms=scenario.Time()-started,
        observed_slow_ms=released.at_ms-accepted.at_ms,is_caster_isolation_verified=true,
        is_splash_damage_and_slow_verified=true,is_nearby_friendly_isolation_verified=true,
        is_native_server_movement_measured=true,is_packet_parity_verified=false})
end
return function()
    check(scenario.RequireAPI(1))
    local source,target,ally,splash=ownedPair(must(scenario.StartServer({})))
    local clients={source,target,ally,splash}
    local pids,instances={},{}
    for _,client in ipairs(clients) do
        if pids[client.PID] or instances[client.InstanceID] then error("owned2v2 identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Dazing Shot hotbar requires1280x720") end
    end
    ensureHero(target,700180862)
    ensureHero(ally,700180862);ensureHero(splash,700180862)
    for _,case in ipairs(cases) do runCase(case,source,target,ally,splash) end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=read(client,befores[client.ID])
        if final.world.game_id~=befores[source.ID].world.game_id
            or final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native2v2 state invalid") end
        must(client.UI:Screenshot("dazing-final-"..client.ID))
        record({event="final_actor",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
            object_id=final.world.hero.object_id,noun=final.world.hero.noun,game_id=final.world.game_id,
            native_frame=final.native.frame_sequence,peer_generation=final.world.peer_generation,
            transport_generation=final.world.transport_generation})
    end
    record({event="suite_end",passed=4,source_pid=source.PID,target_pid=target.PID,
        source_instance=source.InstanceID,target_instance=target.InstanceID,is_all_four_clients_live=true,
        is_all_rank_parity_verified=false,is_full_ability_parity_verified=false})
end

