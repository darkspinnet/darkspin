---Build-103 Zetawatt Beam native observations. Native defaults; no authored-rank fixture.
---Owned 2v2 only; all unknown input outcomes stop without replay.
local must,check=scenario.Must,scenario.Check
local sourceID,targetID="pvp-slow-source","pvp-slow-primary"
local allyID,splashID="pvp-slow-ally","pvp-slow-splash"
local cases={
    {id="beam-meditron-beta",noun=185979955,name="Meditron Beta"},
    {id="beam-seraph-alpha",noun=820281267,name="Seraph-XS Alpha"},
    {id="beam-titan-gamma",noun=1341857832,name="Titan Gamma"},
    {id="beam-srs-delta",noun=2038499613,name="SRS-42 Delta"},
    {id="beam-goliath-alpha",noun=3376462303,name="Goliath Alpha"},
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
    event.suite,event.at_ms="pvp_beam",scenario.Time()
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
local function runCast(case,phase,source,target,ally,splash,isOffset)
    local sourcePin,targetPin,allyPin,splashPin=read(source),read(target),read(ally),read(splash)
    for _,client in ipairs({source,target,ally,splash}) do reset(client) end
    clearRank(source,sourcePin)
    local center=sourcePin.native.position
    place(target,offset(center,5,0),targetPin)
    place(splash,offset(center,isOffset and 5 or 7,isOffset and -2 or 0),splashPin)
    place(ally,offset(center,2,0),allyPin)
    local beforeSource,beforeTarget,beforeAlly,beforeSplash=actor(source,sourcePin),actor(target,targetPin),actor(ally,allyPin),actor(splash,splashPin)
    record({event="case_start",case_id=case.id,phase=phase,asset="TechRandom",ability_name="Zetawatt Beam",
        noun=case.noun,hero_name=case.name,is_native_default_rank=true,is_target_identity_verified=false,
        is_aligned_two_hostiles=not isOffset,is_ally_on_line=true,
        is_ground_aim_intended=phase=="ground-aim"})
    for _,entry in ipairs({beforeSource,beforeTarget,beforeAlly,beforeSplash}) do
        entry.event,entry.case_id,entry.phase="actor_before",case.id,phase;record(entry)
    end
    local started=scenario.Time()
    local button=must(source.UI:Click(605,655))
    if not button.is_processed then error("native random hotbar unprocessed; no replay") end
    pause(50)
    local aim=beforeTarget.native_position
    if phase=="ground-aim" then
        aim=offset(center,12,0)
        click(source,aim)
    else
        click(source,{x=aim.x,y=aim.y,z=aim.z+1.5})
    end
    local paid
    local deadline=started+2500
    while scenario.Time()<deadline do
        local current=read(source,sourcePin)
        if current.world.hero.mana_point<beforeSource.mp then paid=current;break end
        pause(25)
    end
    if not paid then error("native beam mana acceptance unobserved; no replay") end
    -- Observe the .233s impact and .5s release without issuing another input.
    local remaining=started+800-scenario.Time()
    if remaining>0 then pause(math.ceil(remaining)) end
    local afterSource,afterTarget,afterAlly,afterSplash=actor(source,sourcePin),actor(target,targetPin),actor(ally,allyPin),actor(splash,splashPin)
    local damage=beforeTarget.hp-afterTarget.hp
    local splashDamage=beforeSplash.hp-afterSplash.hp
    local sourceDamage=beforeSource.hp-afterSource.hp
    local allyDamage=beforeAlly.hp-afterAlly.hp
    if sourceDamage~=0 or allyDamage~=0 then error("beam caster/friendly damage observed") end
    if isOffset and splashDamage~=0 then error("beam damaged off-axis hostile") end
    local isPositive=damage>0 and (isOffset or splashDamage>0)
    record({event="cast_result",case_id=case.id,phase=phase,
        status=isPositive and "passed" or "requires_trace_for_zero_damage",
        primary_damage=damage,second_damage=splashDamage,source_damage=sourceDamage,ally_damage=allyDamage,
        power_spent=beforeSource.mp-afterSource.mp,elapsed_ms=scenario.Time()-started,
        is_defense_confirmed=false,is_native_default_rank=true,is_native_target_identity_verified=false,
        is_beam_receiving_packet_verified=false,is_vertical_precision_verified=false,
        is_off_axis_exclusion=isOffset,is_ground_aim_intended=phase=="ground-aim"})
    for _,entry in ipairs({afterSource,afterTarget,afterAlly,afterSplash}) do
        entry.event,entry.case_id,entry.phase="actor_after",case.id,phase;record(entry)
    end
    return isPositive
end
return function()
    check(scenario.RequireAPI(1))
    local source,target,ally,splash=ownedPair(must(scenario.StartServer({})))
    local clients={source,target,ally,splash}
    local pids,instances={},{}
    for _,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("beam hotbar requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned native identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
    end
    ensureHero(target,700180862);ensureHero(ally,700180862);ensureHero(splash,700180862)
    local passed=0
    for _,case in ipairs(cases) do
        ensureHero(source,case.noun)
        if runCast(case,"aligned",source,target,ally,splash,false) then passed=passed+1 end
    end
    if runCast(cases[#cases],"off-axis",source,target,ally,splash,true) then passed=passed+1 end
    if runCast(cases[#cases],"ground-aim",source,target,ally,splash,false) then passed=passed+1 end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client) end
    pause(100)
    for _,client in ipairs(clients) do
        local observed=read(client,befores[client.ID])
        if observed.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
        local final=actor(client,befores[client.ID]);final.event="final_actor";record(final)
        local capture=must(client.Diagnostics:Capture("beam-final"))
        record({event="diagnostic",client_id=client.ID,manifest=capture.manifest_path})
        must(client.UI:Screenshot("beam-final-"..client.ID))
    end
    record({event="suite_end",completed=7,passed=passed,is_four_clients_live=true,
        is_native_default_rank=true,is_all_rank_parity_verified=false,
        is_receiving_packet_parity_verified=false,is_vertical_precision_verified=false})
end

