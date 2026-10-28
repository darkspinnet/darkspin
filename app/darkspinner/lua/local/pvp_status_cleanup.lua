---Recipient death during Meteor Strike must retire its target-owned stun.
---Run after pvp_status.lua on the same explicitly owned native duel.
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
    event.suite,event.at_ms="pvp_status_cleanup",scenario.Time()
    local line=json(event)
    if #line>2048 then error("cleanup observation exceeds log boundary") end
    check(scenario.Log(line))
end
local function living(client,pin,isActorChangeAllowed)
    local world=must(client.World:SnapshotLiving())
    if not world.arena or not world.arena.is_combat_active or #world.arena.members~=2
        or not world.hero or world.hero.hit_point<=0 or world.hero.is_stunned==nil then
        error("owned living active duel unavailable")
    end
    if pin and (world.game_id~=pin.game_id or world.peer_generation~=pin.peer_generation
        or world.transport_generation~=pin.transport_generation
        or (not isActorChangeAllowed and (world.hero.object_id~=pin.hero.object_id
            or world.hero.index~=pin.hero.index or world.hero.noun~=pin.hero.noun))) then
        error("cleanup crossed a pinned owned actor/realm; no input replay")
    end
    return world
end
local function native(client)
    local receipt=must(client.World:SnapshotNative())
    if receipt.pid~=client.PID or receipt.instance_id~=client.InstanceID then error("native process identity changed") end
    return receipt.gameplay_state
end
local function readyNative(client)
    local state=native(client)
    if not state or not state.is_available or not state.is_world_active or not state.position
        or not state.is_rig_observed or not state.is_rig_ready then error("owned native rig unavailable") end
    return state
end
local function processed(receipt,label)
    if not receipt.is_processed then error(label.." native edge unprocessed; no replay") end
end
local function reset(client)
    local resource=must(client.World:RestoreHero())
    local cooldown=must(client.World:ResetCooldowns())
    if not resource.is_queued or resource.recipient_count~=2 or not cooldown.is_cooldown_reset
        or cooldown.recipient_count~=2 then error("fixture not published to both owned clients") end
end
local function ensureSlotZero(client)
    local world=living(client)
    if world.hero.is_stunned then error("previous stun must expire before cleanup setup") end
    if world.hero.index~=0 then
        must(client.World:ResetCooldowns())
        must(client.World:SelectHero(0,20000))
    end
end
local function waitArrival(client,pin)
    local deadline=scenario.Time()+8000
    while scenario.Time()<deadline do
        local world=living(client,pin)
        if world.hero.input_lock_remaining_ms==nil then error("actual arrival lock producer unavailable") end
        if world.hero.input_lock_remaining_ms==0 then return world end
        pause(75)
    end
    error("native reserve arrival did not finish")
end
return function()
    check(scenario.RequireAPI(1))
    local source=must(scenario.GetClient("pvp-semantics-source"))
    local target=must(scenario.GetClient("pvp-semantics-target"))
    ensureSlotZero(source);ensureSlotZero(target)
    if living(source).hero.noun~=576185321 then
        local receipt=must(source.World:ReplaceHero(576185321))
        if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count~=2 then
            error("Zrin Alpha replacement did not establish owned native rig")
        end
    end
    reset(source);reset(target)
    local sourcePin,targetPin=living(source),living(target)
    if sourcePin.game_id~=targetPin.game_id then error("owned clients are in different duels") end
    for _,client in ipairs({source,target}) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("cleanup hotbar requires owned1280x720 windows") end
        processed(must(client.World:Click(readyNative(client).position)),"setup stop")
    end
    waitArrival(source,sourcePin);waitArrival(target,targetPin)
    local sourceBefore=readyNative(source)
    local targetBefore=readyNative(target)
    local point=offset(sourceBefore.position,2.5,0)
    local placement=must(target.World:PlaceHero(point))
    if not placement.is_queued or placement.recipient_count~=2 then error("owned target placement incomplete") end
    local deadline=scenario.Time()+5000
    local placed
    while scenario.Time()<deadline do
        living(source,sourcePin);living(target,targetPin)
        local state=readyNative(target)
        if state.frame_sequence>targetBefore.frame_sequence and distance(state.position,point)<.2 then placed=state;break end
        pause(75)
    end
    if not placed then error("native melee-range placement unobserved") end
    record({event="case_start",source_id=source.ID,source_pid=source.PID,source_instance=source.InstanceID,
        target_id=target.ID,target_pid=target.PID,target_instance=target.InstanceID,game_id=sourcePin.game_id,
        source_object=sourcePin.hero.object_id,target_object=targetPin.hero.object_id,target_index=targetPin.hero.index,
        target_noun=targetPin.hero.noun,source_noun=576185321,asset="PlasmaRandom",ability_name="Meteor Strike"})
    processed(must(source.UI:Click(605,655)),"Meteor Strike hotbar")
    pause(150)
    processed(must(source.World:Click(placed.position)),"Meteor Strike aim")
    deadline=scenario.Time()+4000
    local stunnedAt,stunnedWorld
    while scenario.Time()<deadline do
        living(source,sourcePin)
        local world=living(target,targetPin)
        if world.hero.is_stunned and world.hero.hit_point<targetPin.hero.hit_point then
            stunnedAt,stunnedWorld=scenario.Time(),world;break
        end
        pause(40)
    end
    if not stunnedAt then error("accepted Meteor Strike damage/stun unobserved; no replay") end
    record({event="stun_observed",damage=targetPin.hero.hit_point-stunnedWorld.hero.hit_point,
        target_object=targetPin.hero.object_id,target_frame=readyNative(target).frame_sequence})
    local resource=must(target.World:RestoreHero({hit_point=1}))
    if not resource.is_queued or resource.recipient_count~=2 then error("one-HP living fixture incomplete") end
    local wounded=living(target,targetPin)
    if not wounded.hero.is_stunned or math.abs(wounded.hero.hit_point-1)>.01 then
        error("one-HP fixture did not preserve active accepted stun")
    end
    local body=readyNative(target).position
    local aim={x=body.x,y=body.y,z=body.z+1.5}
    local attack=must(source.World:Hold(aim,700))
    local click=attack.gameplay_click
    if not attack.is_processed or not click or not click.is_press_poll_observed or not click.is_release_poll_observed then
        error("ordinary native basic input incomplete; no replay")
    end
    deadline=stunnedAt+2200 -- Leaves a margin inside the authored three-second stun.
    local replacement,identityAt
    while scenario.Time()<deadline do
        living(source,sourcePin)
        local world=living(target,targetPin,true)
        if world.hero.index~=targetPin.hero.index and world.hero.object_id~=targetPin.hero.object_id then
            replacement,identityAt=world,scenario.Time();break
        end
        pause(40)
    end
    if not replacement then error("inconclusive: ordinary basic did not reveal reserve identity within active stun window") end
    if replacement.hero.is_stunned then error("defeated recipient stun leaked into automatic reserve") end
    record({event="lethal_transition",old_object=targetPin.hero.object_id,old_index=targetPin.hero.index,
        new_object=replacement.hero.object_id,new_index=replacement.hero.index,new_noun=replacement.hero.noun,
        elapsed_since_stun_ms=identityAt-stunnedAt,is_stunned=replacement.hero.is_stunned})
    local rigDeadline=scenario.Time()+8000
    local replacementNative
    while scenario.Time()<rigDeadline do
        local world=living(target,replacement)
        if world.hero.is_stunned then error("reserve inherited defeated actor's stun") end
        local state=native(target)
        if state and state.is_available and state.is_world_active and state.position and state.is_rig_observed
            and state.is_rig_ready and state.controlled_handle~=placed.controlled_handle
            and state.frame_sequence>placed.frame_sequence then replacementNative=state;break end
        pause(75)
    end
    if not replacementNative then error("automatic reserve native rig/frame unobserved") end
    waitArrival(target,replacement)
    local moveBefore=readyNative(target)
    local move=must(target.World:Hold(offset(moveBefore.position,0,5),650))
    processed(move,"reserve movement")
    deadline=scenario.Time()+2500
    local moved
    while scenario.Time()<deadline do
        local world=living(target,replacement)
        if world.hero.is_stunned then error("reserve status reappeared after death cleanup") end
        local state=readyNative(target)
        if state.frame_sequence>moveBefore.frame_sequence and distance(state.position,moveBefore.position)>.5 then moved=state;break end
        pause(75)
    end
    if not moved then error("automatic reserve native movement unavailable") end
    processed(must(target.World:Click(moved.position)),"reserve stop")
    local sourceFinalBefore,targetFinalBefore=readyNative(source),readyNative(target)
    pause(100)
    living(source,sourcePin);living(target,replacement)
    local sourceFinal,targetFinal=readyNative(source),readyNative(target)
    if sourceFinal.frame_sequence<=sourceFinalBefore.frame_sequence or targetFinal.frame_sequence<=targetFinalBefore.frame_sequence then
        error("independent final native frames did not advance")
    end
    must(source.UI:Screenshot("meteor-cleanup-source"));must(target.UI:Screenshot("meteor-cleanup-reserve"))
    record({event="suite_end",status="passed",source_pid=source.PID,target_pid=target.PID,
        elapsed_since_stun_to_reserve_ms=identityAt-stunnedAt,is_defeated_stun_retired=true,
        is_reserve_native_rig_ready=true,is_reserve_movement_verified=true,is_both_clients_live=true,
        is_packet_parity_verified=false})
end
