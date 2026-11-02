---Retained owned2v2: ordinary recipient snare switches and source rank-fixture lifetime.
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
    event.suite,event.at_ms="pvp_silence_swap",scenario.Time()
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
local function restore(client)
    local receipt=must(client.World:RestoreHero())
    if not receipt.is_queued or receipt.recipient_count~=4 then error("resource fixture missing owned recipient") end
end
local function rankFixture(client,pin,rank)
    read(client,pin)
    local receipt=must(client.World:SetAbilityRank(rank))
    if not receipt.is_ability_rank_set or not receipt.is_server_rank_fixture or receipt.is_queued
        or receipt.ability_rank~=rank or receipt.object_id~=pin.world.hero.object_id or receipt.recipient_count~=4 then
        error("server-only rank fixture not established")
    end
    record({event="server_rank_fixture",rank=rank,client_id=client.ID,pid=client.PID,
        object_id=pin.world.hero.object_id,is_native_rank_selection=false})
end
local function place(client,pin,point)
    local before=read(client,pin)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=4 then error("placement missing owned recipient") end
    local deadline=scenario.Time()+5000
    while scenario.Time()<deadline do
        local observed=read(client,pin)
        if observed.native.frame_sequence>before.native.frame_sequence and distance(observed.native.position,point)<.2 then return end
        pause(50)
    end
    error("native placement deadline")
end
local function actor(client,pin,phase,rank)
    local observed=read(client,pin)
    record({event="actor",phase=phase,rank=rank,client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        game_id=observed.world.game_id,object_id=observed.world.hero.object_id,noun=observed.world.hero.noun,
        index=observed.world.hero.index,peer_generation=observed.world.peer_generation,
        transport_generation=observed.world.transport_generation,native_frame=observed.native.frame_sequence,
        native_ability_ranks=observed.native.ability_ranks,is_native_rank_available=observed.native.is_ability_rank_available,
        hp=observed.world.hero.hit_point,power=observed.world.hero.mana_point,
        movement_modifier=observed.world.hero.hostile_movement_modifier,is_silenced=observed.world.hero.is_silenced})
    return observed
end
local function resetCooldown(client)
    local receipt=must(client.World:ResetCooldowns())
    if not receipt.is_cooldown_reset or receipt.recipient_count~=4 then error("cooldown fixture incomplete") end
end
local function support(source,sourcePin,primary,primaryPin,splash,splashPin,isSilenced,phase)
    restore(source);restore(primary);restore(splash);resetCooldown(source)
    local beforeFirst,beforeSecond=read(primary,primaryPin),read(splash,splashPin)
    local started=scenario.Time()
    local button=must(source.UI:Click(640,655))
    if not button.is_processed then error("native support hotbar unprocessed; no replay") end
    pause(50)
    local point=beforeFirst.native.position
    click(source,{x=point.x,y=point.y,z=point.z+1.5})
    local deadline=scenario.Time()+2200
    while scenario.Time()<deadline do
        local first,second=read(primary,primaryPin),read(splash,splashPin)
        local isExpected=first.world.hero.is_silenced==isSilenced and second.world.hero.is_silenced==isSilenced
        if not isSilenced then isExpected=isExpected
            and math.abs(first.world.hero.hostile_movement_modifier+.5)<.001
            and math.abs(second.world.hero.hostile_movement_modifier+.5)<.001 end
        if isExpected and first.world.hero.hit_point<beforeFirst.world.hero.hit_point
            and second.world.hero.hit_point<beforeSecond.world.hero.hit_point then
            record({event="support_accepted",phase=phase,is_silenced=isSilenced,
                primary_damage=beforeFirst.world.hero.hit_point-first.world.hero.hit_point,
                splash_damage=beforeSecond.world.hero.hit_point-second.world.hero.hit_point,
                native_input_started_ms=started,is_native_rank_selection=false})
            return first
        end
        pause(25)
    end
    error("native support damage/status unobserved: "..phase.."; no replay")
end
local function select(client,pin,slot,phase)
    read(client,pin)
    resetCooldown(client)
    local receipt=must(client.World:SelectHero(slot,20000))
    local observed=read(client)
    if observed.world.hero.index~=slot or observed.world.hero.object_id==pin.world.hero.object_id
        or observed.native.controlled_handle==pin.native.controlled_handle then error("native squad identity did not change") end
    if observed.world.game_id~=pin.world.game_id or observed.world.peer_generation~=pin.world.peer_generation
        or observed.world.transport_generation~=pin.world.transport_generation then error("squad switch crossed realm") end
    record({event="ordinary_squad_switch",phase=phase,client_id=client.ID,pid=client.PID,
        previous_object=pin.world.hero.object_id,object_id=observed.world.hero.object_id,index=slot,
        noun=observed.world.hero.noun,is_silenced=observed.world.hero.is_silenced,
        movement_modifier=observed.world.hero.hostile_movement_modifier,
        is_stunned=observed.world.hero.is_stunned,is_native_selection_processed=receipt~=nil,
        is_old_modifier_retirement_packet_verified=false})
    return observed
end
local function active(primary,primaryPin,source,sourcePin)
    restore(primary);restore(source);resetCooldown(primary)
    local before,victim=read(primary,primaryPin),read(source,sourcePin)
    local button=must(primary.UI:Click(605,655))
    if not button.is_processed then error("native active hotbar unprocessed; no replay") end
    pause(50)
    local point=victim.native.position
    click(primary,{x=point.x,y=point.y,z=point.z+1.5})
    local deadline=scenario.Time()+3500
    while scenario.Time()<deadline do
        local after,afterVictim=read(primary,primaryPin),read(source,sourcePin)
        if after.world.hero.mana_point<before.world.hero.mana_point-1
            and afterVictim.world.hero.hit_point<victim.world.hero.hit_point then
            record({event="returned_recipient_active_accepted",power_spent=before.world.hero.mana_point-after.world.hero.mana_point,
                actual_damage=victim.world.hero.hit_point-afterVictim.world.hero.hit_point})
            return
        end
        pause(40)
    end
    error("returned native recipient active did not spend power and damage; no replay")
end
return function()
    check(scenario.RequireAPI(1))
    local source,primary=must(scenario.GetClient(sourceID)),must(scenario.GetClient(targetID))
    local ally,splash=must(scenario.GetClient(allyID)),must(scenario.GetClient(splashID))
    local clients={source,primary,ally,splash}
    local pins,pids,instances={},{},{}
    local expectedNouns={3068979135,700180862,1194511925,700180862}
    for index,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("swap probes require1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
        local current=read(client)
        if current.world.hero.index~=0 then
            resetCooldown(client)
            must(client.World:SelectHero(0,20000))
        end
        pins[client.ID]=read(client)
        if pins[client.ID].world.hero.index~=0 or pins[client.ID].world.hero.noun~=expectedNouns[index]
            or pins[client.ID].world.game_id~=pins[source.ID].world.game_id then error("retained owned2v2 actor mismatch") end
        rankFixture(client,pins[client.ID],0)
        actor(client,pins[client.ID],"initial",0)
    end
    local center=read(source,pins[source.ID]).native.position
    place(ally,pins[ally.ID],offset(center,-5,0));place(primary,pins[primary.ID],offset(center,5,0))
    place(splash,pins[splash.ID],offset(center,5,-2))
    rankFixture(source,pins[source.ID],1)
    local initial=support(source,pins[source.ID],primary,pins[primary.ID],splash,pins[splash.ID],false,"recipient-snare-before-switch")
    local original=pins[primary.ID]
    local reserve=select(primary,original,1,"recipient-away")
    if reserve.world.hero.is_silenced or reserve.world.hero.is_stunned
        or math.abs(reserve.world.hero.hostile_movement_modifier)>.001 then error("status followed recipient into reserve") end
    if reserve.at_ms-initial.at_ms>=5000 then error("recipient reserve arrived after original snare deadline; retirement inconclusive") end
    record({event="recipient_reserve_clear",
        recipient_status="snare",elapsed_since_status_ms=reserve.at_ms-initial.at_ms,
        is_original_five_second_window=reserve.at_ms-initial.at_ms<5000,
        is_old_modifier_retirement_packet_verified=false})
    local returned=select(primary,reserve,0,"recipient-return")
    if returned.world.hero.object_id~=original.world.hero.object_id or returned.world.hero.noun~=original.world.hero.noun
        or returned.world.hero.is_silenced or returned.world.hero.is_stunned
        or math.abs(returned.world.hero.hostile_movement_modifier)>.001 then error("old recipient status survived ordinary away/back") end
    pins[primary.ID]=returned
    record({event="returned_recipient_clear",recipient_status="snare",elapsed_since_status_ms=returned.at_ms-initial.at_ms,
        is_original_five_second_window=returned.at_ms-initial.at_ms<5000,
        is_old_modifier_retirement_packet_verified=false})
    active(primary,returned,source,pins[source.ID])
    local deadline=scenario.Time()+4500
    while read(source,pins[source.ID]).world.hero.is_stunned and scenario.Time()<deadline do pause(50) end
    if read(source,pins[source.ID]).world.hero.is_stunned then error("native active stun did not expire") end
    rankFixture(source,pins[source.ID],5)
    local sourceOriginal=pins[source.ID]
    local sourceReserve=select(source,sourceOriginal,1,"fixture-source-away")
    local sourceReturned=select(source,sourceReserve,0,"fixture-source-return")
    if sourceReturned.world.hero.object_id~=sourceOriginal.world.hero.object_id
        or sourceReturned.world.hero.noun~=sourceOriginal.world.hero.noun then error("source original identity did not return") end
    pins[source.ID]=sourceReturned
    actor(source,sourceReturned,"after-source-switch",0)
    support(source,sourceReturned,primary,pins[primary.ID],splash,pins[splash.ID],false,"source-fixture-retired-base-snare")
    record({event="source_fixture_retirement_verified",is_base_snare_observed=true,is_silence_observed=false,
        is_native_rank_selection=false,is_server_rank_fixture_reactivation_observed=false})
    rankFixture(source,sourceReturned,0)
    deadline=scenario.Time()+7000
    local isNormal=false
    while scenario.Time()<deadline do
        isNormal=true
        for _,client in ipairs(clients) do
            local observed=read(client,pins[client.ID])
            if observed.world.hero.is_silenced or observed.world.hero.is_stunned
                or math.abs(observed.world.hero.hostile_movement_modifier)>.001 then isNormal=false end
        end
        if isNormal then break end
        pause(50)
    end
    if not isNormal then error("final statuses did not expire") end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client,pins[client.ID]) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=actor(client,pins[client.ID],"final",0)
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
    end
    must(source.UI:Screenshot("silence-swap-source"));must(primary.UI:Screenshot("silence-swap-primary"))
    record({event="suite_end",is_four_clients_live=true,is_source_fixture_retirement_verified=true,
        is_recipient_snare_reserve_and_return_clear=true,is_old_modifier_retirement_packet_verified=false,
        is_active_silence_swap_verified=false,
        is_native_rank_selection_verified=false,is_full_ability_parity_verified=false})
end
