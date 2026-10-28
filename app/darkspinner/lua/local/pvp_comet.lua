---Build-103 Comet native push observations; owned2v2, native defaults, no replay.
local must,check=scenario.Must,scenario.Check
local sourceID,targetID="pvp-slow-source","pvp-slow-primary"
local allyID,splashID="pvp-slow-ally","pvp-slow-splash"
local selectedCaseIDs={} -- Empty covers every owner; select IDs for focused reruns.
local allCases={
    {id="comet-maldri-gamma",noun=2902294899,name="Maldri Gamma"},
    {id="comet-orion-delta",noun=3255874076,name="Orion Delta"},
    {id="comet-magnos-beta",noun=3639041301,name="Magnos Beta"},
    {id="comet-andromeda-alpha",noun=3877795735,name="Andromeda Alpha"},
    {id="comet-vex-gamma",noun=4026895060,name="Vex Gamma"},
}
local cases={}
for _,case in ipairs(allCases) do
    if next(selectedCaseIDs)==nil or selectedCaseIDs[case.id] then cases[#cases+1]=case end
end
if #cases==0 then error("no selected Celestial Comet cases") end
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
    event.suite,event.at_ms="pvp_comet",scenario.Time()
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

local function actor(client,pin)
    local observed=read(client,pin)
    local hero=observed.world.hero
    return {client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        object_id=hero.object_id,noun=hero.noun,index=hero.index,game_id=observed.world.game_id,
        peer_generation=observed.world.peer_generation,transport_generation=observed.world.transport_generation,
        hp=hero.hit_point,mp=hero.mana_point,native_position=observed.native.position,
        server_position=hero.movement_position,native_frame=observed.native.frame_sequence,
        native_ability_ranks=observed.native.ability_ranks}
end
local function clearRank(source,pin)
    local receipt=must(source.World:SetAbilityRank(0))
    if not receipt.is_ability_rank_set or not receipt.is_server_rank_fixture or receipt.ability_rank~=0
        or receipt.object_id~=pin.world.hero.object_id or receipt.recipient_count~=4 then
        error("native-default rank fixture clear unavailable")
    end
end
local function position(observed)
    local hero=observed.world.hero
    if not hero.is_movement_available or not hero.movement_position then error("sampled server motion unavailable") end
    return hero.movement_position
end
local function probeCorridor(clients,pins,center,candidate)
    local impact=offset(center,5,0)
    local points={center,offset(impact,0,5.8),offset(impact,-2.5,-.5),offset(impact,0,-5.8)}
    local befores={}
    for index,client in ipairs(clients) do
        befores[index]=read(client,pins[index])
        local receipt=must(client.World:PlaceHero(points[index]))
        if not receipt.is_queued or receipt.recipient_count~=4 then error("corridor placement receipt unknown; stop") end
    end
    local settledAt=scenario.Time()+200
    local deadline=scenario.Time()+1500
    local afters
    while scenario.Time()<deadline do
        local current={}
        local areFramesReady=true
        for index,client in ipairs(clients) do
            current[index]=read(client,pins[index])
            if current[index].native.frame_sequence<befores[index].native.frame_sequence+2 then areFramesReady=false end
        end
        if areFramesReady and scenario.Time()>=settledAt then
            -- Require a second stable observation, rather than interpreting an
            -- old queued frame as the native terrain projection.
            pause(75)
            local areStable=true
            local stable={}
            for index,client in ipairs(clients) do
                stable[index]=read(client,pins[index])
                if stable[index].native.frame_sequence<=current[index].native.frame_sequence
                    or distance(stable[index].native.position,current[index].native.position)>.05
                    or math.abs(stable[index].native.position.z-current[index].native.position.z)>.05 then areStable=false end
            end
            if areStable then afters=stable;break end
        end
        pause(30)
    end
    if not afters then error("corridor native frames did not settle; no cast or fixture replay") end
    local isReachable=true
    for index,client in ipairs(clients) do
        local observed=afters[index]
        local xyError=distance(observed.native.position,points[index])
        local zError=math.abs(observed.native.position.z-center.z)
        if xyError>.2 or zError>.5 then isReachable=false end
        record({event="corridor_probe",candidate=candidate,client_id=client.ID,pid=client.PID,
            requested=points[index],native_position=observed.native.position,server_position=position(observed),
            xy_error=xyError,z_from_anchor=zError,native_frame=observed.native.frame_sequence,
            frame_advance=observed.native.frame_sequence-befores[index].native.frame_sequence,
            is_fixture_prep=true,is_cast_performed=false})
    end
    return isReachable
end
local function prepareCorridor(source,target,ally,splash)
    local clients={source,target,ally,splash}
    local pins={read(source),read(target),read(ally),read(splash)}
    local anchor=pins[1].native.position
    local offsets={{0,0},{-8,0},{0,-8},{8,0},{0,8}}
    for index,delta in ipairs(offsets) do
        local center=offset(anchor,delta[1],delta[2])
        if probeCorridor(clients,pins,center,index) then
            record({event="corridor_ready",candidate=index,center=center,is_fixture_prep=true,is_cast_performed=false})
            return
        end
        record({event="corridor_clamped",candidate=index,is_fixture_prep=true,is_cast_performed=false})
    end
    error("no verified flat Celestial Comet landing corridor in bounded fixture anchors; no casts")
end
local function isolation(client,pin,before)
    local after=read(client,pin)
    if after.world.hero.hit_point~=before.world.hero.hit_point or after.world.hero.is_stunned
        or distance(after.native.position,before.native.position)>.3 then error("Comet caster/friendly affected") end
end
local function pushResult(case,client,pin,before,after,impact)
    local damage=before.world.hero.hit_point-after.world.hero.hit_point
    local nativeDistance=distance(before.native.position,after.native.position)
    local serverDistance=distance(position(before),position(after))
    local dx,dy=before.native.position.x-impact.x,before.native.position.y-impact.y
    local radius=math.sqrt(dx*dx+dy*dy)
    local nativeDot,serverDot=0,0
    if nativeDistance>0 then
        nativeDot=((after.native.position.x-before.native.position.x)*dx
            +(after.native.position.y-before.native.position.y)*dy)/(radius*nativeDistance)
    end
    if serverDistance>0 then
        serverDot=((position(after).x-position(before).x)*dx
            +(position(after).y-position(before).y)*dy)/(radius*serverDistance)
    end
    record({event="push_result",case_id=case.id,client_id=client.ID,pid=client.PID,
        object_id=pin.world.hero.object_id,damage=damage,impact=impact,
        native_before=before.native.position,native_after=after.native.position,
        server_before=position(before),server_after=position(after),
        native_distance=nativeDistance,server_distance=serverDistance,
        native_outward_dot=nativeDot,server_outward_dot=serverDot,
        status=damage>0 and "positive_damage_requires_motion" or "requires_trace_for_zero_damage",
        is_defense_confirmed=false,is_actual_command_target_verified=false})
    if damage<=0 then return false end
    if nativeDistance<3.2 or nativeDistance>4.8 or serverDistance<3.2 or serverDistance>4.8
        or nativeDot<.85 or serverDot<.85 then
        error("Comet damage accepted but four-unit impact-origin push missing; inspect terrain/native trace")
    end
    return true
end
local function runCase(case,source,target,ally,splash)
    local sourcePin=ensureHero(source,case.noun)
    local targetPin,allyPin,splashPin=read(target),read(ally),read(splash)
    for _,client in ipairs({source,target,ally,splash}) do reset(client) end
    clearRank(source,sourcePin)
    local center=sourcePin.native.position
    local impact=offset(center,5,0)
    place(target,offset(impact,0,1.8),targetPin)
    place(splash,offset(impact,0,-1.8),splashPin)
    place(ally,offset(impact,-2.5,-.5),allyPin)
    local beforeSource,beforeTarget,beforeAlly,beforeSplash=read(source,sourcePin),read(target,targetPin),read(ally,allyPin),read(splash,splashPin)
    record({event="case_start",case_id=case.id,hero_name=case.name,noun=case.noun,
        asset="SpacetimeRandom3",ability_name="Celestial Comet",impact=impact,is_ground_aim_intended=true,
        is_native_default_rank=true,is_actual_command_target_verified=false})
    for _,client in ipairs({source,target,ally,splash}) do
        local entry=actor(client);entry.event,entry.case_id="actor_before",case.id;record(entry)
    end
    local started=scenario.Time()
    local button=must(source.UI:Click(605,655))
    if not button.is_processed then error("native Comet hotbar unprocessed; no replay") end
    pause(50);click(source,impact)
    local sawTargetStun,sawSplashStun=false,false
    local afterTarget,afterSplash
    local deadline=started+2200
    while scenario.Time()<deadline do
        local first,second,caster=read(target,targetPin),read(splash,splashPin),read(source,sourcePin)
        sawTargetStun=sawTargetStun or first.world.hero.is_stunned
        sawSplashStun=sawSplashStun or second.world.hero.is_stunned
        isolation(source,sourcePin,beforeSource);isolation(ally,allyPin,beforeAlly)
        record({event="push_sample",case_id=case.id,elapsed_ms=scenario.Time()-started,
            primary_hp=first.world.hero.hit_point,splash_hp=second.world.hero.hit_point,
            primary_stunned=first.world.hero.is_stunned,splash_stunned=second.world.hero.is_stunned,
            primary_native=first.native.position,splash_native=second.native.position,
            primary_server=position(first),splash_server=position(second)})
        local isDamage=first.world.hero.hit_point<beforeTarget.world.hero.hit_point
            or second.world.hero.hit_point<beforeSplash.world.hero.hit_point
        if isDamage and caster.world.hero.mana_point<beforeSource.world.hero.mana_point
            and scenario.Time()-started>=1000 and not first.world.hero.is_stunned and not second.world.hero.is_stunned then
            afterTarget,afterSplash=first,second;break
        end
        pause(35)
    end
    if not afterTarget then error("Comet positive damage/payment/released motion unobserved; no replay") end
    local firstPositive=pushResult(case,target,targetPin,beforeTarget,afterTarget,impact)
    local secondPositive=pushResult(case,splash,splashPin,beforeSplash,afterSplash,impact)
    if firstPositive and not sawTargetStun or secondPositive and not sawSplashStun then
        error("accepted Comet push lacks observed server motion gate")
    end
    -- Probe native recovery only after the real motion gate has ended.
    local recoveryClient,recoveryPin,recoveryBefore=target,targetPin,afterTarget
    if not firstPositive then recoveryClient,recoveryPin,recoveryBefore=splash,splashPin,afterSplash end
    holdMovement(recoveryClient,offset(recoveryBefore.native.position,-3,0),350,case.id,"recovery")
    local recoveryAfter=read(recoveryClient,recoveryPin)
    local nativeRecovery=distance(recoveryBefore.native.position,recoveryAfter.native.position)
    local serverRecovery=distance(position(recoveryBefore),position(recoveryAfter))
    record({event="recovery",case_id=case.id,client_id=recoveryClient.ID,
        native_distance=nativeRecovery,server_distance=serverRecovery,is_stunned=recoveryAfter.world.hero.is_stunned})
    if nativeRecovery<.5 or serverRecovery<.5 or recoveryAfter.world.hero.is_stunned then error("native movement did not recover after Comet") end
    record({event="case_end",case_id=case.id,
        status=firstPositive and secondPositive and "passed" or "requires_trace_for_zero_damage",
        elapsed_ms=scenario.Time()-started,is_impact_origin_verified=true,is_friendly_isolation_verified=true,
        is_actual_command_target_verified=false,is_receiver_parity_verified=false})
    return firstPositive and secondPositive
end
return function()
    check(scenario.RequireAPI(1))
    local source,target,ally,splash=ownedPair(must(scenario.StartServer({})))
    local clients={source,target,ally,splash}
    local pids,instances={},{}
    for _,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Comet hotbar requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned native identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
    end
    ensureHero(target,700180862);ensureHero(ally,700180862);ensureHero(splash,700180862)
    ensureHero(source,cases[1].noun)
    prepareCorridor(source,target,ally,splash)
    local passed=0
    for _,case in ipairs(cases) do if runCase(case,source,target,ally,splash) then passed=passed+1 end end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=read(client,befores[client.ID])
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
        local entry=actor(client,befores[client.ID]);entry.event="final_actor";record(entry)
        local capture=must(client.Diagnostics:Capture("comet-final"))
        record({event="diagnostic",client_id=client.ID,manifest=capture.manifest_path})
        must(client.UI:Screenshot("comet-final-"..client.ID))
    end
    record({event="suite_end",completed=#cases,passed=passed,is_four_clients_live=true,
        is_all_rank_parity_verified=false,is_receiver_parity_verified=false})
end

