---Retained owned2v2: eight ordinary Dazing Shot casts under explicit SERVER rank fixtures.
---Reuses one owned 2v2 match; inputs with unknown outcomes stop.
local must,check = scenario.Must,scenario.Check
local sourceID,targetID = "pvp-slow-source","pvp-slow-primary"
local allyID,splashID = "pvp-slow-ally","pvp-slow-splash"
local selectedRanks = {} -- Empty runs1..8; explicit entries select independent future coverage casts.
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
    event.suite,event.at_ms="pvp_ranked",scenario.Time()
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
local function castRank(clients,pins,rank)
    local source,primary,ally,splash=clients[1],clients[2],clients[3],clients[4]
    restore(source);restore(primary);restore(splash)
    local cooldown=must(source.World:ResetCooldowns())
    if not cooldown.is_cooldown_reset or cooldown.recipient_count~=4 then error("cooldown fixture incomplete") end
    rankFixture(source,pins[source.ID],rank)
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=actor(client,pins[client.ID],"before",rank) end
    local started=scenario.Time()
    local target=befores[primary.ID].native.position
    local button=must(source.UI:Click(640,655))
    if not button.is_processed then error("native support edge unprocessed; no replay") end
    pause(50)
    click(source,{x=target.x,y=target.y,z=target.z+1.5})
    record({event="native_input_processed",rank=rank,source_object=pins[source.ID].world.hero.object_id,
        target_object=pins[primary.ID].world.hero.object_id,input_started_at_ms=started,
        is_native_rank_selection=false,is_native_ability_identity_verified=false,
        status_policy="intentional overlapping statuses and cross-rank reapplication; no forced status reset"})
    local deadline=scenario.Time()+2000
    local accepted
    while scenario.Time()<deadline do
        local first,second=read(primary,pins[primary.ID]),read(splash,pins[splash.ID])
        local caster=read(source,pins[source.ID])
        if caster.world.hero.mana_point<befores[source.ID].world.hero.mana_point
            and (first.world.hero.hit_point<befores[primary.ID].world.hero.hit_point
                or second.world.hero.hit_point<befores[splash.ID].world.hero.hit_point) then accepted=first;break end
        pause(25)
    end
    if not accepted then
        local caster,first,second=read(source,pins[source.ID]),read(primary,pins[primary.ID]),read(splash,pins[splash.ID])
        record({event="rank_inconclusive",rank=rank,status="requires_trace_for_zero_damage",
            power_spent=befores[source.ID].world.hero.mana_point-caster.world.hero.mana_point,
            primary_damage=befores[primary.ID].world.hero.hit_point-first.world.hero.hit_point,
            splash_damage=befores[splash.ID].world.hero.hit_point-second.world.hero.hit_point,
            is_defense_confirmed=false,is_native_rank_selection=false})
        record({event="retry_rank",rank=rank,reason="power and at least one positive victim damage not jointly observed",is_replayed=false})
        error("ranked cast requires trace inspection; no replay")
    end
    local remaining=started+550-scenario.Time()
    if remaining>0 then pause(math.max(1,math.ceil(remaining))) end
    local afters={}
    for _,client in ipairs(clients) do afters[client.ID]=actor(client,pins[client.ID],"after",rank) end
    for _,client in ipairs({source,ally}) do
        if afters[client.ID].world.hero.hit_point~=befores[client.ID].world.hero.hit_point
            or afters[client.ID].world.hero.is_silenced
            or math.abs(afters[client.ID].world.hero.hostile_movement_modifier)>.001 then error("ranked cast hit a friendly") end
    end
    local cost=befores[source.ID].world.hero.mana_point-afters[source.ID].world.hero.mana_point
    if cost<=0 then error("ranked cast did not spend power") end
    local primaryDamage=befores[primary.ID].world.hero.hit_point-afters[primary.ID].world.hero.hit_point
    local splashDamage=befores[splash.ID].world.hero.hit_point-afters[splash.ID].world.hero.hit_point
    local isBothPositive=primaryDamage>0 and splashDamage>0
    local status=isBothPositive and "passed" or "requires_trace_for_zero_damage"
    record({event="rank_result",rank=rank,status=status,source_object=pins[source.ID].world.hero.object_id,
        primary_object=pins[primary.ID].world.hero.object_id,splash_object=pins[splash.ID].world.hero.object_id,
        primary_damage=primaryDamage,splash_damage=splashDamage,is_both_victim_damage_positive=isBothPositive,
        power_before=befores[source.ID].world.hero.mana_point,power_after=afters[source.ID].world.hero.mana_point,
        power_spent=cost,expected_cost_formula="12 + 0.06 * primary attribute; attribute not exposed here",
        elapsed_ms=scenario.Time()-started,is_native_rank_selection=false,is_friendly_isolation_verified=true,
        is_defense_confirmed=false})
    if not isBothPositive then
        record({event="retry_rank",rank=rank,reason="positive damage coverage missing on one victim; inspect receiving traces",is_replayed=false})
    end
    return isBothPositive
end
return function()
    check(scenario.RequireAPI(1))
    local source=must(scenario.GetClient(sourceID))
    local primary=must(scenario.GetClient(targetID))
    local ally=must(scenario.GetClient(allyID))
    local splash=must(scenario.GetClient(splashID))
    local clients={source,primary,ally,splash}
    local pins,pids,instances={},{},{}
    local expectedNouns={3068979135,700180862,1194511925,700180862}
    for index,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("ranked batch requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
        pins[client.ID]=read(client)
        if pins[client.ID].world.hero.index~=0 or pins[client.ID].world.hero.noun~=expectedNouns[index]
            or pins[client.ID].world.game_id~=pins[source.ID].world.game_id then error("retained owned2v2 actor mismatch") end
        rankFixture(client,pins[client.ID],0)
    end
    local center=read(source,pins[source.ID]).native.position
    place(ally,pins[ally.ID],offset(center,-5,0))
    place(primary,pins[primary.ID],offset(center,5,0))
    place(splash,pins[splash.ID],offset(center,5,-2))
    local ranks=selectedRanks
    if #ranks==0 then ranks={1,2,3,4,5,6,7,8} end
    local seenRanks,retryRanks={},{}
    local passed=0
    for _,rank in ipairs(ranks) do
        if rank<1 or rank>8 or rank~=math.floor(rank) or seenRanks[rank] then error("invalid selected rank") end
        seenRanks[rank]=true
        if castRank(clients,pins,rank) then passed=passed+1 else retryRanks[#retryRanks+1]=rank end
    end
    rankFixture(source,pins[source.ID],0)
    must(source.Diagnostics:Capture("ranked-completed-casts"))
    local deadline=scenario.Time()+7500
    local isNormal=false
    while scenario.Time()<deadline do
        isNormal=true
        for _,client in ipairs(clients) do
            local observed=read(client,pins[client.ID])
            if observed.world.hero.is_silenced or observed.world.hero.is_stunned
                or math.abs(observed.world.hero.hostile_movement_modifier)>.001
                or math.abs(observed.world.hero.hostile_attack_modifier)>.001 then isNormal=false end
        end
        if isNormal then break end
        pause(50)
    end
    if not isNormal then error("final ranked statuses failed to expire naturally") end
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client,pins[client.ID]) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=actor(client,pins[client.ID],"final",0)
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
    end
    must(source.UI:Screenshot("ranked-final-source"));must(primary.UI:Screenshot("ranked-final-primary"))
    record({event="suite_end",passed=passed,completed=#ranks,retry_ranks=retryRanks,
        requires_trace_count=#retryRanks,is_four_clients_live=true,is_all_statuses_expired=true,
        is_server_fixture_rank_coverage=true,is_native_rank_selection_verified=false,is_full_ability_parity_verified=false})
end
