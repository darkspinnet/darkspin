---Build-103 silence probes using explicit server-only authored-rank fixtures.
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
    event.suite,event.at_ms="pvp_silence",scenario.Time()
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
        or world.hero.hit_point<=0 or world.hero.is_silenced==nil or world.hero.hostile_movement_modifier==nil or world.hero.hostile_attack_modifier==nil or not native
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
        ally=function() ally=launch(server,allyID,1194511925);return nil end,
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
local function restore(client)
    local receipt=must(client.World:RestoreHero())
    if not receipt.is_queued or receipt.recipient_count~=4 then error("resources missing owned recipient") end
end
local function setRank(client,rank)
    local before=read(client)
    local receipt=must(client.World:SetAbilityRank(rank))
    if not receipt.is_ability_rank_set or not receipt.is_server_rank_fixture or receipt.is_queued
        or receipt.ability_rank~=rank or receipt.recipient_count~=4
        or receipt.object_id~=before.world.hero.object_id then error("server rank fixture not established") end
    record({event="server_rank_fixture",client_id=client.ID,pid=client.PID,rank=rank,
        object_id=before.world.hero.object_id,is_native_rank_selection=false})
end
local function ledger(client,pin,phase)
    local observed=read(client,pin)
    if not observed.native.is_ability_rank_available or not observed.native.ability_ranks
        or #observed.native.ability_ranks~=9 then error("independent native rank observer unavailable") end
    record({event="actor",phase=phase,client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        object_id=observed.world.hero.object_id,noun=observed.world.hero.noun,index=observed.world.hero.index,
        game_id=observed.world.game_id,peer_generation=observed.world.peer_generation,
        transport_generation=observed.world.transport_generation,native_frame=observed.native.frame_sequence,
        native_ability_ranks=observed.native.ability_ranks,is_silenced=observed.world.hero.is_silenced,
        hp=observed.world.hero.hit_point,power=observed.world.hero.mana_point})
    return observed
end
local function cast(client,pin,target,targetPin,x,phase)
    read(client,pin)
    local observed=read(target,targetPin)
    local button=must(client.UI:Click(x,655))
    if not button.is_processed then error("native hotbar unprocessed; no replay") end
    pause(50)
    click(client,{x=observed.native.position.x,y=observed.native.position.y,z=observed.native.position.z+1.5})
    record({event="native_input_processed",phase=phase,client_id=client.ID,pid=client.PID,
        target_object=observed.world.hero.object_id,hotbar_x=x,is_native_rank_selection=false,
        is_native_ability_identity_verified=false})
end
local function pairSilenced(primary,primaryPin,splash,splashPin,isExpected,deadline,phase)
    while scenario.Time()<deadline do
        local first,second=read(primary,primaryPin),read(splash,splashPin)
        if first.world.hero.is_silenced==isExpected and second.world.hero.is_silenced==isExpected then
            record({event="silence_pair",phase=phase,is_silenced=isExpected,
                primary_hp=first.world.hero.hit_point,splash_hp=second.world.hero.hit_point,
                primary_object=first.world.hero.object_id,splash_object=second.world.hero.object_id})
            return first
        end
        pause(25)
    end
    error("paired silence transition deadline: "..phase.."; no replay")
end
local function isolated(source,sourcePin,ally,allyPin)
    if read(source,sourcePin).world.hero.is_silenced or read(ally,allyPin).world.hero.is_silenced then
        error("silence crossed friendly boundary")
    end
end
local function acceptedSupport(caster,pin,primary,primaryPin,splash,splashPin,phase)
    local beforeFirst,beforeSecond=read(primary,primaryPin),read(splash,splashPin)
    cast(caster,pin,primary,primaryPin,640,phase)
    local deadline=scenario.Time()+1800
    while scenario.Time()<deadline do
        local first,second=read(primary,primaryPin),read(splash,splashPin)
        if first.world.hero.is_silenced and second.world.hero.is_silenced
            and first.world.hero.hit_point<beforeFirst.world.hero.hit_point
            and second.world.hero.hit_point<beforeSecond.world.hero.hit_point then
            record({event="support_accepted",phase=phase,source_object=pin.world.hero.object_id,
                damage=beforeFirst.world.hero.hit_point-first.world.hero.hit_point,
                splash_damage=beforeSecond.world.hero.hit_point-second.world.hero.hit_point})
            return first
        end
        pause(25)
    end
    error("native support damage/silence unobserved: "..phase.."; no replay")
end
local function abilityBlocked(primary,pin,source,sourcePin,deadline)
    local before=read(primary,pin)
    cast(primary,pin,source,sourcePin,605,"silenced-random-attempt")
    local untilAt=scenario.Time()+500
    while scenario.Time()<untilAt do
        local observed=read(primary,pin)
        if not observed.world.hero.is_silenced or observed.world.hero.mana_point~=before.world.hero.mana_point then
            error("silenced active attempt spent power or silence expired inside gate window")
        end
        pause(40)
    end
    if scenario.Time()>=deadline then error("silence gate window exceeded intended lifetime") end
    record({event="active_blocked",power_before=before.world.hero.mana_point,
        power_after=read(primary,pin).world.hero.mana_point,is_server_gate_observed=true,
        is_native_ability_identity_verified=false,is_packet_acceptance_verified=false})
    -- Clear any retained native aiming selection through the ordinary cancel producer.
    local receipt=must(primary.Ability:Cancel())
    if not receipt.is_processed then error("native aim cancellation unprocessed") end
end
local function basicAndMovement(primary,pin,source,sourcePin,phase)
    local before=read(primary,pin)
    if not before.world.hero.is_silenced then error("movement probe began after silence") end
    holdMovement(primary,offset(before.native.position,0,6),350,phase,"silenced-movement")
    local moved=read(primary,pin)
    local displacement=distance(before.native.position,moved.native.position)
    if not moved.world.hero.is_silenced or displacement<.5 then error("silence prevented ordinary native movement") end
    restore(source)
    local victim=read(source,sourcePin)
    local basicBefore=read(primary,pin)
    holdMovement(primary,{x=victim.native.position.x,y=victim.native.position.y,z=victim.native.position.z+1.5},650,phase,"silenced-basic")
    click(primary,read(primary,pin).native.position)
    local deadline=scenario.Time()+700
    while scenario.Time()<deadline do
        local observed,afterVictim=read(primary,pin),read(source,sourcePin)
        if not observed.world.hero.is_silenced then error("basic result arrived after silence; inconclusive") end
        if afterVictim.world.hero.hit_point<victim.world.hero.hit_point then
            record({event="silenced_basic_and_movement",phase=phase,native_displacement=displacement,
                basic_damage=victim.world.hero.hit_point-afterVictim.world.hero.hit_point,
                power_before=basicBefore.world.hero.mana_point,power_after=observed.world.hero.mana_point,
                is_silenced=true})
            return
        end
        pause(30)
    end
    error("ordinary basic damage unobserved during silence")
end
local function activeAfterExpiry(primary,pin,source,sourcePin,phase)
    restore(source);reset(primary)
    local before,victim=read(primary,pin),read(source,sourcePin)
    if before.world.hero.is_silenced then error("active recovery attempted before silence expiry") end
    cast(primary,pin,source,sourcePin,605,phase.."-active-recovery")
    local deadline=scenario.Time()+3500
    while scenario.Time()<deadline do
        local observed,afterVictim=read(primary,pin),read(source,sourcePin)
        if observed.world.hero.mana_point<before.world.hero.mana_point-1
            and afterVictim.world.hero.hit_point<victim.world.hero.hit_point then
            record({event="active_recovered",phase=phase,power_spent=before.world.hero.mana_point-observed.world.hero.mana_point,
                actual_damage=victim.world.hero.hit_point-afterVictim.world.hero.hit_point})
            return
        end
        pause(40)
    end
    error("post-silence native active power/damage unobserved; no replay")
end
local function runCase(clients,pins,firstRank,refreshRank,duration,isGateCase)
    local source,primary,ally,splash=clients[1],clients[2],clients[3],clients[4]
    local sourcePin,primaryPin,allyPin,splashPin=pins[source.ID],pins[primary.ID],pins[ally.ID],pins[splash.ID]
    local phase="rank"..firstRank.."-refresh"..refreshRank
    local center=read(source,sourcePin).native.position
    for _,client in ipairs(clients) do reset(client) end
    place(ally,offset(center,-5,0),allyPin);place(primary,offset(center,5,0),primaryPin);place(splash,offset(center,5,-2),splashPin)
    setRank(source,firstRank);setRank(ally,refreshRank)
    record({event="case_start",phase=phase,server_initial_rank=firstRank,server_refresh_rank=refreshRank,
        expected_retained_duration_ms=duration,is_native_rank_selection=false})
    pairSilenced(primary,primaryPin,splash,splashPin,false,scenario.Time()+500,"clean-boundary")
    local accepted=acceptedSupport(source,sourcePin,primary,primaryPin,splash,splashPin,phase.."-initial")
    local initialAt=accepted.at_ms
    if isGateCase then abilityBlocked(primary,primaryPin,source,sourcePin,initialAt+2700) end
    restore(primary);restore(splash)
    local refreshed=acceptedSupport(ally,allyPin,primary,primaryPin,splash,splashPin,phase.."-refresh")
    local refreshedAt=refreshed.at_ms
    record({event="refresh_observed",phase=phase,elapsed_since_initial_ms=refreshedAt-initialAt,
        expected_retained_duration_ms=duration,is_instance_identity_verified=false})
    isolated(source,sourcePin,ally,allyPin)
    if isGateCase then basicAndMovement(primary,primaryPin,source,sourcePin,phase) end
    local released=pairSilenced(primary,primaryPin,splash,splashPin,false,refreshedAt+duration+1700,phase.."-expiry")
    local elapsed=released.at_ms-refreshedAt
    if elapsed<duration-350 or elapsed>duration+650 then error("refresh failed to retain original silence duration") end
    isolated(source,sourcePin,ally,allyPin)
    activeAfterExpiry(primary,primaryPin,source,sourcePin,phase)
    record({event="case_end",phase=phase,status="passed",observed_after_refresh_ms=elapsed,
        is_native_rank_selection=false,is_instance_identity_verified=false})
    for _,client in ipairs(clients) do
        local receipt=must(client.Diagnostics:Capture("silence-"..phase))
        record({event="diagnostic",phase=phase,client_id=client.ID,manifest=receipt.manifest_path})
    end
    local deadline=scenario.Time()+4000
    while read(source,sourcePin).world.hero.is_stunned and scenario.Time()<deadline do pause(50) end
    if read(source,sourcePin).world.hero.is_stunned then error("prior native active stun did not expire") end
end
return function()
    check(scenario.RequireAPI(1))
    local source,primary,ally,splash=ownedPair(must(scenario.StartServer({})))
    local clients={source,primary,ally,splash}
    ensureHero(source,3068979135);ensureHero(ally,1194511925)
    ensureHero(primary,700180862);ensureHero(splash,700180862)
    local pins,pids,instances={},{},{}
    for _,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("silence hotbar requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("native identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
        pins[client.ID]=read(client)
        setRank(client,0)
        ledger(client,pins[client.ID],"native-rank-baseline")
    end
    runCase(clients,pins,5,6,3000,true)
    runCase(clients,pins,6,5,1000,false)
    for _,client in ipairs(clients) do setRank(client,0) end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client,pins[client.ID]) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=ledger(client,pins[client.ID],"final")
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("native final frame stalled") end
        must(client.UI:Screenshot("silence-final-"..client.ID))
    end
    record({event="suite_end",passed=2,is_four_clients_live=true,is_server_fixture_rank_coverage=true,
        is_native_rank_selection_verified=false,is_silence_instance_packet_parity_verified=false})
end
