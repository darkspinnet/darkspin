---Standalone CasterUnique Dazing Shot follow-up on pvp_slow.lua's owned 2v2.
local must,check=scenario.Must,scenario.Check
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
    event.suite,event.at_ms="pvp_slow_stack",scenario.Time()
    local line=json(event)
    if #line>2048 then error("slow-stack log exceeds durable boundary") end
    check(scenario.Log(line))
end
local function read(client,pin)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if probe.pid~=client.PID or probe.instance_id~=client.InstanceID or not world.arena
        or not world.arena.is_combat_active or #world.arena.members~=4 or not world.hero or world.hero.hit_point<=0
        or world.hero.hostile_movement_modifier==nil or world.hero.hostile_attack_modifier==nil
        or not native or not native.is_world_active or not native.position or not native.is_rig_ready then
        error("owned living four-client Arena/rig observation unavailable")
    end
    if pin and (world.game_id~=pin.world.game_id or world.peer_generation~=pin.world.peer_generation
        or world.transport_generation~=pin.world.transport_generation or world.hero.object_id~=pin.world.hero.object_id
        or world.hero.index~=pin.world.hero.index or world.hero.noun~=pin.world.hero.noun
        or native.controlled_handle~=pin.native.controlled_handle) then error("slow-stack actor/realm changed; no replay") end
    return {world=world,native=native,at_ms=scenario.Time()}
end
local function isModifier(observed,movement)
    return math.abs(observed.world.hero.hostile_movement_modifier-movement)<.001
        and math.abs(observed.world.hero.hostile_attack_modifier)<.001
end
local function processed(receipt,label)
    if not receipt.is_processed then error(label.." native edge unprocessed; no replay") end
end
local function restore(client)
    local receipt=must(client.World:RestoreHero())
    if not receipt.is_queued or receipt.recipient_count~=4 then error("resource fixture missing owned recipient") end
end
local function reset(client)
    restore(client)
    local receipt=must(client.World:ResetCooldowns())
    if not receipt.is_cooldown_reset or receipt.recipient_count~=4 then error("cooldown fixture missing owned recipient") end
end
local function ensure(client,noun)
    local world=must(client.World:SnapshotLiving())
    if world.hero.index~=0 then must(client.World:ResetCooldowns());must(client.World:SelectHero(0,20000)) end
    if must(client.World:SnapshotLiving()).hero.noun~=noun then
        local receipt=must(client.World:ReplaceHero(noun))
        if not receipt.is_rig_ready or receipt.recipient_count~=4 then error("native replacement rig unobserved") end
    end
    return read(client)
end
local function place(client,point,pin)
    local before=read(client,pin)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=4 then error("placement missing owned recipient") end
    local deadline=scenario.Time()+5000
    while scenario.Time()<deadline do
        local observed=read(client,pin)
        if observed.native.frame_sequence>before.native.frame_sequence and distance(observed.native.position,point)<.2 then return observed end
        pause(75)
    end
    error("fresh native placement unobserved")
end
local function isolated(clients,pins)
    for _,client in ipairs(clients) do
        local observed=read(client,pins[client.ID])
        if not isModifier(observed,0) then error("Dazing Shot status crossed team/range boundary") end
    end
end
local function waitModifier(target,pin,movement,deadline,phase)
    while scenario.Time()<deadline do
        local observed=read(target,pin)
        if isModifier(observed,movement) then
            record({event="modifier_observed",phase=phase,modifier=movement,target_frame=observed.native.frame_sequence,
                target_hp=observed.world.hero.hit_point,target_object=observed.world.hero.object_id})
            return observed
        end
        pause(50)
    end
    error("bounded modifier transition unobserved: "..phase.."; no cast replay")
end
local function cast(caster,casterPin,target,targetPin,movement,phase)
    local before=read(target,targetPin)
    read(caster,casterPin)
    processed(must(caster.UI:Click(640,655)),"Dazing Shot hotbar")
    pause(150)
    local point=before.native.position
    processed(must(caster.World:Click({x=point.x,y=point.y,z=point.z+1.5})),"Dazing Shot aim")
    local deadline=scenario.Time()+2500
    while scenario.Time()<deadline do
        read(caster,casterPin)
        local observed=read(target,targetPin)
        if isModifier(observed,movement) and observed.world.hero.hit_point<before.world.hero.hit_point then
            record({event="cast_observed",phase=phase,source_id=caster.ID,source_pid=caster.PID,
                source_object=casterPin.world.hero.object_id,damage=before.world.hero.hit_point-observed.world.hero.hit_point,
                movement_modifier=movement,target_frame=observed.native.frame_sequence})
            return observed
        end
        pause(50)
    end
    error("accepted native cast damage/modifier unobserved: "..phase.."; no replay")
end
return function()
    check(scenario.RequireAPI(1))
    local source=must(scenario.GetClient("pvp-slow-source"))
    local ally=must(scenario.GetClient("pvp-slow-ally"))
    local target=must(scenario.GetClient("pvp-slow-primary"))
    local splash=must(scenario.GetClient("pvp-slow-splash"))
    local clients={source,ally,target,splash}
    ensure(source,3068979135);ensure(ally,1194511925);ensure(target,700180862)
    local pins={}
    local gameID
    for _,client in ipairs(clients) do
        reset(client)
        local observed=read(client)
        if gameID and observed.world.game_id~=gameID then error("owned slow clients do not share a match") end
        gameID=observed.world.game_id
        pins[client.ID]=observed
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Dazing Shot coordinates require owned1280x720 windows") end
        processed(must(client.World:Click(observed.native.position)),"setup stop")
        record({event="actor",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,game_id=gameID,
            object_id=observed.world.hero.object_id,noun=observed.world.hero.noun,index=observed.world.hero.index,
            peer_generation=observed.world.peer_generation,transport_generation=observed.world.transport_generation})
    end
    local center=read(source,pins[source.ID]).native.position
    place(ally,offset(center,0,3),pins[ally.ID])
    place(target,offset(center,5,0),pins[target.ID])
    place(splash,offset(center,15,0),pins[splash.ID])
    waitModifier(target,pins[target.ID],0,scenario.Time()+6500,"initial-clean-boundary")
    isolated({source,ally,splash},pins)
    local first=cast(source,pins[source.ID],target,pins[target.ID],-.5,"first-caster")
    restore(target) -- Resources only; accepted target-owned status is preserved.
    pause(700)
    local second=cast(ally,pins[ally.ID],target,pins[target.ID],-.9,"second-caster")
    restore(target)
    pause(700)
    local cooldown=must(source.World:ResetCooldowns())
    if not cooldown.is_cooldown_reset or cooldown.recipient_count~=4 then error("recast cooldown reset incomplete") end
    local refreshed=cast(source,pins[source.ID],target,pins[target.ID],-.9,"first-caster-refresh")
    isolated({source,ally,splash},pins)
    local deadline=refreshed.at_ms+7000
    local single
    while scenario.Time()<deadline do
        local observed=read(target,pins[target.ID])
        if isModifier(observed,-.5) then single=observed;break end
        if not isModifier(observed,-.9) then error("CasterUnique refresh lost or added an independent slow") end
        pause(50)
    end
    if not single or single.at_ms-second.at_ms<4300 then error("independent caster expired too early, possibly stale first-caster callback") end
    record({event="independent_expiry",modifier=-.5,elapsed_since_first_ms=single.at_ms-first.at_ms,
        elapsed_since_second_ms=single.at_ms-second.at_ms,elapsed_since_refresh_ms=single.at_ms-refreshed.at_ms})
    local normal=waitModifier(target,pins[target.ID],0,deadline,"last-caster-expiry")
    if normal.at_ms-refreshed.at_ms<4300 then error("refreshed caster status expired prematurely") end
    isolated({source,ally,splash},pins)
    local before=read(target,pins[target.ID])
    local movement=must(target.World:Hold(offset(before.native.position,0,5),650))
    local poll=movement.gameplay_click
    if not movement.is_processed or not poll or not poll.is_press_poll_observed or not poll.is_release_poll_observed then
        error("final native movement lacks paired input polls; no replay")
    end
    deadline=scenario.Time()+2500
    local moved
    while scenario.Time()<deadline do
        local observed=read(target,pins[target.ID])
        if not isModifier(observed,0) then error("expired slow reappeared") end
        if observed.native.frame_sequence>before.native.frame_sequence and distance(observed.native.position,before.native.position)>.5 then moved=observed;break end
        pause(75)
    end
    if not moved then error("final native target movement unobserved") end
    processed(must(target.World:Click(moved.native.position)),"final target stop")
    local frames={}
    for _,client in ipairs(clients) do frames[client.ID]=read(client,pins[client.ID]).native.frame_sequence end
    pause(100)
    for _,client in ipairs(clients) do
        local observed=read(client,pins[client.ID])
        if observed.native.frame_sequence<=frames[client.ID] then error("independent native frame did not advance") end
        must(client.UI:Screenshot("slow-stack-final-"..client.ID))
        record({event="final_actor",client_id=client.ID,pid=client.PID,game_id=gameID,
            object_id=observed.world.hero.object_id,frame=observed.native.frame_sequence,is_live=true})
    end
    record({event="suite_end",status="passed",is_two_caster_aggregate_verified=true,
        is_same_caster_refresh_verified=true,is_independent_expiry_verified=true,is_final_native_movement_verified=true,
        is_four_clients_live=true,is_packet_parity_verified=false})
end
