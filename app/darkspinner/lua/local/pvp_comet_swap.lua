---Retained owned2v2 Celestial Comet recipient/caster lifetime observations.
local must,check=scenario.Must,scenario.Check
local sourceID,targetID="pvp-slow-source","pvp-slow-primary"
local allyID,splashID="pvp-slow-ally","pvp-slow-splash"
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
    event.suite,event.at_ms="pvp_comet_swap",scenario.Time()
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
local function actor(client,pin,phase)
    local observed=read(client,pin)
    local hero=observed.world.hero
    record({event="actor",phase=phase,client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        game_id=observed.world.game_id,object_id=hero.object_id,noun=hero.noun,index=hero.index,
        peer_generation=observed.world.peer_generation,transport_generation=observed.world.transport_generation,
        native_frame=observed.native.frame_sequence,controlled_handle=observed.native.controlled_handle,
        native_position=observed.native.position,server_position=hero.movement_position,
        hp=hero.hit_point,mp=hero.mana_point,is_stunned=hero.is_stunned})
    return observed
end
local function replaceDuringFlight(client,pin,noun,phase)
    actor(client,pin,phase.."-before")
    record({event="replacement_start",phase=phase,client_id=client.ID,pid=client.PID,
        old_object_id=pin.world.hero.object_id,old_noun=pin.world.hero.noun,new_noun=noun})
    -- ReplaceHero is an owned fixture with native retirement/readiness; its
    -- documented API replaces the deployed slot using a single noun argument.
    local receipt=must(client.World:ReplaceHero(noun))
    if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count~=4 then
        error("replacement outcome unknown; no replay")
    end
    local after=actor(client,nil,phase.."-after")
    if after.world.hero.noun~=noun or after.world.hero.index~=0 or after.world.hero.is_stunned then
        error("replacement actor inherited old recipient motion or wrong identity")
    end
    record({event="replacement_end",phase=phase,client_id=client.ID,pid=client.PID,
        old_object_id=pin.world.hero.object_id,old_noun=pin.world.hero.noun,
        new_object_id=after.world.hero.object_id,new_noun=after.world.hero.noun,
        is_native_rig_ready=after.native.is_rig_ready,is_stunned=after.world.hero.is_stunned})
    return after
end
local function verifyLanded(client,pin,before,impact,phase)
    local deadline=scenario.Time()+2500
    local after
    while scenario.Time()<deadline do
        after=read(client,pin)
        if not after.world.hero.is_stunned then break end
        pause(25)
    end
    local nativeDistance=distance(before.native.position,after.native.position)
    local nativeDelta={x=after.native.position.x-before.native.position.x,y=after.native.position.y-before.native.position.y}
    local outward={x=before.native.position.x-impact.x,y=before.native.position.y-impact.y}
    local radius=math.sqrt(outward.x*outward.x+outward.y*outward.y)
    local outwardDot=0
    if nativeDistance>0 then outwardDot=(nativeDelta.x*outward.x+nativeDelta.y*outward.y)/(nativeDistance*radius) end
    record({event="recipient_landed",phase=phase,client_id=client.ID,pid=client.PID,
        object_id=after.world.hero.object_id,noun=after.world.hero.noun,
        native_before=before.native.position,native_after=after.native.position,
        server_after=after.world.hero.movement_position,is_stunned=after.world.hero.is_stunned,
        native_distance=nativeDistance,outward_dot=outwardDot,
        damage=before.world.hero.hit_point-after.world.hero.hit_point})
    if after.world.hero.is_stunned or nativeDistance<3.2 or nativeDistance>4.8 or outwardDot<.85 then
        error("surviving recipient lost its accepted impact-origin push lifecycle")
    end
    return after
end
local function runCase(phase,source,target,ally,splash,isReplaceTarget)
    local clients={source,target,ally,splash}
    local sourcePin,targetPin,allyPin,splashPin=read(source),read(target),read(ally),read(splash)
    for _,client in ipairs(clients) do reset(client) end
    local center=sourcePin.native.position
    local impact=offset(center,5,0)
    place(target,offset(impact,0,1.8),targetPin)
    place(splash,offset(impact,0,-1.8),splashPin)
    place(ally,offset(impact,-2.5,-.5),allyPin)
    local beforeTarget,beforeSplash=read(target,targetPin),read(splash,splashPin)
    local beforeSource,beforeAlly=read(source,sourcePin),read(ally,allyPin)
    for _,client in ipairs(clients) do actor(client,nil,phase.."-cast-before") end
    record({event="cast_start",phase=phase,ability_name="Celestial Comet",asset="SpacetimeRandom3",
        impact=impact,is_ground_aim_intended=true,is_actual_command_target_verified=false})
    local started=scenario.Time()
    local button=must(source.UI:Click(605,655))
    if not button.is_processed then error("native Comet hotbar unprocessed; no replay") end
    pause(50);click(source,impact)
    local trigger
    local deadline=started+1500
    while scenario.Time()<deadline do
        local first,second=read(target,targetPin),read(splash,splashPin)
        if first.world.hero.hit_point<beforeTarget.world.hero.hit_point
            and second.world.hero.hit_point<beforeSplash.world.hero.hit_point
            and first.world.hero.is_stunned and second.world.hero.is_stunned
            and distance(beforeTarget.native.position,first.native.position)<3.2 then trigger=first;break end
        if scenario.Time()-started>650 then break end
        pause(15)
    end
    if not trigger then error("both positive victims and in-flight trigger unobserved; no replay") end
    record({event="flight_trigger",phase=phase,elapsed_ms=trigger.at_ms-started,
        target_object_id=trigger.world.hero.object_id,target_noun=trigger.world.hero.noun,
        target_native=trigger.native.position,is_stunned=trigger.world.hero.is_stunned,
        native_travel_so_far=distance(beforeTarget.native.position,trigger.native.position)})
    if isReplaceTarget then
        local replaced=replaceDuringFlight(target,targetPin,3068979135,phase)
        verifyLanded(splash,splashPin,beforeSplash,impact,phase)
        -- Observe the replacement beyond the old run's full flight/outro.
        pause(200)
        local stable=actor(target,replaced,phase.."-replacement-stable")
        if stable.world.hero.is_stunned or distance(stable.native.position,replaced.native.position)>.2 then
            error("retired recipient callback changed replacement actor")
        end
    else
        replaceDuringFlight(source,sourcePin,2902294899,phase)
        verifyLanded(target,targetPin,beforeTarget,impact,phase)
        verifyLanded(splash,splashPin,beforeSplash,impact,phase)
    end
    local friend=read(ally,allyPin)
    local caster=read(source)
    if friend.world.hero.hit_point~=beforeAlly.world.hero.hit_point or friend.world.hero.is_stunned
        or distance(friend.native.position,beforeAlly.native.position)>.3 or caster.world.hero.is_stunned then
        error("Comet caster/friendly isolation failed")
    end
    if isReplaceTarget and caster.world.hero.hit_point~=beforeSource.world.hero.hit_point then error("Comet damaged its caster") end
    record({event="case_end",phase=phase,status="passed",elapsed_ms=scenario.Time()-started,
        is_replacement_requested_after_inflight_observation=true,is_packet_lifecycle_verified=false})
end
return function()
    check(scenario.RequireAPI(1))
    local source,target=must(scenario.GetClient(sourceID)),must(scenario.GetClient(targetID))
    local ally,splash=must(scenario.GetClient(allyID)),must(scenario.GetClient(splashID))
    local clients={source,target,ally,splash}
    local gameID=read(source).world.game_id
    for _,client in ipairs(clients) do
        local observed=read(client)
        local window=must(client.UI:InspectWindow()).window
        if observed.world.game_id~=gameID or not window or window.width~=1280 or window.height~=720
            or observed.world.hero.index~=0 then error("retained owned match/slot/window mismatch") end
    end
    if read(source).world.hero.noun~=4026895060 then error("expected retained Vex Gamma source; no setup replay") end
    for _,client in ipairs({target,ally,splash}) do
        if read(client).world.hero.noun~=700180862 then error("expected retained Lumin recipient/friendly") end
    end
    runCase("caster-replacement",source,target,ally,splash,false)
    runCase("recipient-replacement",source,target,ally,splash,true)
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=actor(client,befores[client.ID],"final")
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
        local capture=must(client.Diagnostics:Capture("comet-swap-final"))
        record({event="diagnostic",client_id=client.ID,manifest=capture.manifest_path})
        must(client.UI:Screenshot("comet-swap-final-"..client.ID))
    end
    record({event="suite_end",completed=2,passed=2,is_four_clients_live=true,is_packet_lifecycle_verified=false})
end

