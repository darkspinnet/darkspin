---Build-103 Meteor accepted-hit stun on three installed hero families.
---Shares one owned duel with pvp_sphere.lua; inputs with unknown outcomes stop.
local must,check = scenario.Must,scenario.Check
local sourceID,targetID = "pvp-semantics-source","pvp-semantics-target"
local cases = {
    {id="meteor-zrin-alpha",noun=576185321,name="Zrin Alpha"},
    {id="meteor-blitz-beta",noun=454054015,name="Blitz Beta"},
    {id="meteor-krel-gamma",noun=3108182650,name="Krel Gamma"},
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
    event.suite,event.at_ms="pvp_status",scenario.Time()
    local line=json(event)
    if #line>2048 then error("status log exceeds durable boundary") end
    check(scenario.Log(line))
end
local function read(client,pin)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if probe.pid~=client.PID or probe.instance_id~=client.InstanceID or not world.arena
        or not world.arena.is_combat_active or #world.arena.members~=2 or not world.hero
        or world.hero.hit_point<=0 or world.hero.is_stunned==nil or not native
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
    if not resources.is_queued or resources.recipient_count~=2 or not cooldowns.is_cooldown_reset
        or cooldowns.recipient_count~=2 then error("fixture not published to both owned participants") end
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
        if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count~=2 then
            error("replacement did not establish both native rigs")
        end
    end
    return read(client)
end
local function place(client,point,pin)
    local before=read(client,pin)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("placement missing owned recipient") end
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
    local profile=must(server:CreateProfile({name=id,hero_nouns={noun,117394854,3068979135},chain_progression=2,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=id,launch_mode="detached"}))
    must(client:Login());must(client.Scene:Wait(scene.Arsenal))
    return client
end
local function ownedPair(server)
    local source,sourceError=scenario.GetClient(sourceID)
    local target,targetError=scenario.GetClient(targetID)
    if source and target then
        if read(source).world.game_id~=read(target).world.game_id then error("owned clients are in different games") end
        return source,target
    end
    if source or target or not missing(sourceError) or not missing(targetError) then error("partial owned pair; no setup replay") end
    source=launch(server,sourceID,cases[1].noun)
    target=launch(server,targetID,700180862)
    must(source.Party:Create("status"));must(source.Party:Invite(target));must(target.Party:Join(source))
    must(source.Scene:Set(scene.Navigation));must(target.Scene:Set(scene.Navigation))
    must(source:WaitForReportedLocation("navigation",15000));must(target:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{target}}}))
    check(scenario.Parallel({source=function() must(source.PVP:Accept());return nil end,
        target=function() must(target.PVP:Accept());return nil end}))
    must(source.PVP:Wait());must(target.PVP:Wait())
    return source,target
end
local function runCase(case,source,target)
    local sourcePin=ensureHero(source,case.noun)
    local targetPin=read(target)
    click(source,sourcePin.native.position);click(target,targetPin.native.position)
    reset(source);reset(target)
    local center=read(source,sourcePin).native.position
    place(target,offset(center,5,0),targetPin)
    local beforeSource,beforeTarget=read(source,sourcePin),read(target,targetPin)
    local started=scenario.Time()
    record({event="case_start",case_id=case.id,hero_name=case.name,noun=case.noun,asset="PlasmaRandom",
        source_id=source.ID,source_pid=source.PID,target_id=target.ID,target_pid=target.PID,
        game_id=beforeSource.world.game_id,source_object=beforeSource.world.hero.object_id,target_object=beforeTarget.world.hero.object_id})
    local button=must(source.UI:Click(605,655))
    if not button.is_processed then error("native random-ability edge unprocessed; no replay") end
    pause(150)
    click(source,beforeTarget.native.position)
    local accepted,deadline=nil,scenario.Time()+5000
    while scenario.Time()<deadline do
        local observed=read(target,targetPin)
        if observed.world.hero.is_stunned then
            if observed.world.hero.hit_point>=beforeTarget.world.hero.hit_point then error("stun without accepted damage") end
            accepted=observed;break
        end
        pause(50)
    end
    if not accepted then error("Meteor accepted damage/stun unobserved; rerun required without input replay") end
    record({event="stun_observed",case_id=case.id,damage=beforeTarget.world.hero.hit_point-accepted.world.hero.hit_point,
        elapsed_ms=accepted.at_ms-started,target_frame=accepted.native.frame_sequence})
    local goal=offset(accepted.native.position,0,6)
    local blockedUntil=scenario.Time()+450
    holdMovement(target,goal,350,case.id,"blocked")
    local blocked=read(target,targetPin)
    if not blocked.world.hero.is_stunned or distance(blocked.native.position,accepted.native.position)>.2 then
        error("accepted stun failed to block held native movement")
    end
    while scenario.Time()<blockedUntil do
        pause(75)
        local blocked=read(target,targetPin)
        if not blocked.world.hero.is_stunned or distance(blocked.native.position,accepted.native.position)>.2 then
            error("accepted stun failed to block native movement")
        end
    end
    -- The modifier belongs to its recipient; an ordinary caster squad swap
    -- must not prematurely remove the target's already accepted disable.
    if case.id=="meteor-zrin-alpha" then
        must(source.World:ResetCooldowns())
        must(source.World:SelectHero(1,20000))
        local afterSwap=read(target,targetPin)
        if afterSwap.at_ms-accepted.at_ms<2200 and not afterSwap.world.hero.is_stunned then
            error("caster swap prematurely retired target-owned stun")
        end
        record({event="caster_swap",case_id=case.id,is_target_stunned=afterSwap.world.hero.is_stunned,
            elapsed_since_stun_ms=afterSwap.at_ms-accepted.at_ms})
    end
    deadline=accepted.at_ms+5000
    local released
    while scenario.Time()<deadline do
        local observed=read(target,targetPin)
        if not observed.world.hero.is_stunned then released=observed;break end
        pause(60)
    end
    if not released then error("target-owned stun did not expire") end
    local beforeMovement=read(target,targetPin)
    holdMovement(target,offset(beforeMovement.native.position,0,6),650,case.id,"recovery")
    local resumed=read(target,targetPin)
    local displacement=distance(resumed.native.position,beforeMovement.native.position)
    record({event="movement_recovery",case_id=case.id,native_before=beforeMovement.native.position,
        native_after=resumed.native.position,elapsed_ms=resumed.at_ms-beforeMovement.at_ms,
        native_displacement=displacement})
    if resumed.world.hero.is_stunned or resumed.native.frame_sequence<=beforeMovement.native.frame_sequence or displacement<=.5 then
        error("native held movement did not recover after stun expiry")
    end
    record({event="case_end",case_id=case.id,status="passed",elapsed_ms=scenario.Time()-started,
        observed_stun_ms=released.at_ms-accepted.at_ms,is_native_movement_blocked=true,is_native_movement_resumed=true,
        is_packet_parity_verified=false})
end
return function()
    check(scenario.RequireAPI(1))
    local source,target=ownedPair(must(scenario.StartServer({})))
    for _,client in ipairs({source,target}) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Meteor hotbar coordinates require owned1280x720 windows") end
    end
    ensureHero(target,700180862)
    for _,case in ipairs(cases) do runCase(case,source,target) end
    local first,second=read(source),read(target)
    pause(100)
    local firstFinal,secondFinal=read(source),read(target)
    if firstFinal.native.frame_sequence<=first.native.frame_sequence or secondFinal.native.frame_sequence<=second.native.frame_sequence then
        error("final independent native frames stalled")
    end
    must(source.UI:Screenshot("meteor-final-source"));must(target.UI:Screenshot("meteor-final-target"))
    record({event="suite_end",passed=3,source_pid=source.PID,target_pid=target.PID,is_both_clients_live=true,
        is_all_hero_status_parity_verified=false})
end
