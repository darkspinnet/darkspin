---Retained owned2v2: real hostile/friendly projectiles and server-fixture Flak Cannon upgrades.
---Reuses one owned 2v2 match; inputs with unknown outcomes stop.
local must,check = scenario.Must,scenario.Check
local sourceID,targetID = "pvp-slow-source","pvp-slow-primary"
local allyID,splashID = "pvp-slow-ally","pvp-slow-splash"
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
    event.suite,event.at_ms="pvp_flak_projectile",scenario.Time()
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
    record({event="placement_requested",client_id=client.ID,pid=client.PID,
        requested=point,native_before=before.native.position,server_before=before.world.hero.position})
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=4 then error("placement missing owned recipient") end
    local deadline=scenario.Time()+5000
    local last=before
    while scenario.Time()<deadline do
        local observed=read(client,pin)
        last=observed
        if observed.native.frame_sequence>before.native.frame_sequence and distance(observed.native.position,point)<.2
            and math.abs(observed.native.position.z-point.z)<.5 then
            record({event="placement_arrived",client_id=client.ID,pid=client.PID,
                requested=point,native_actual=observed.native.position,server_actual=observed.world.hero.position})
            return
        end
        pause(50)
    end
    record({event="placement_failed",client_id=client.ID,pid=client.PID,requested=point,
        native_actual=last.native.position,server_actual=last.world.hero.position,
        native_xy_error=distance(last.native.position,point),native_z_error=last.native.position.z-point.z})
    error("native placement deadline; requested/actual clamp evidence logged")
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
local function observe(source,pin,phase)
    local observed=read(source,pin)
    local arena,hero=observed.world.arena,observed.world.hero
    if arena.friendly_projectile_count==nil or arena.hostile_projectile_count==nil
        or hero.flak_stack_count==nil or hero.flak_attack_speed==nil then error("real projectile/Flak producers unavailable") end
    record({event="projectile_buff_sample",phase=phase,client_id=source.ID,pid=source.PID,
        object_id=hero.object_id,native_frame=observed.native.frame_sequence,
        friendly_projectile_count=arena.friendly_projectile_count,hostile_projectile_count=arena.hostile_projectile_count,
        flak_stack_count=hero.flak_stack_count,flak_attack_speed=hero.flak_attack_speed,
        power=hero.mana_point,position=observed.native.position})
    return observed
end
local function cooldown(client)
    local receipt=must(client.World:ResetCooldowns())
    if not receipt.is_cooldown_reset or receipt.recipient_count~=4 then error("cooldown fixture incomplete") end
end
local function cast(client,pin,point,x,phase)
    read(client,pin)
    local button=must(client.UI:Click(x,655))
    if not button.is_processed then error("native hotbar unprocessed; no replay") end
    pause(50)
    click(client,point)
    record({event="native_cast_processed",phase=phase,client_id=client.ID,pid=client.PID,
        hotbar_x=x,aim=point,is_native_ability_identity_verified=false,is_native_rank_selection=false})
end
local function waitClean(source,pin,phase,timeout)
    local deadline=scenario.Time()+timeout
    while scenario.Time()<deadline do
        local observed=observe(source,pin,phase)
        if observed.world.arena.friendly_projectile_count==0 and observed.world.arena.hostile_projectile_count==0 then return observed end
        pause(50)
    end
    error("prior projectiles did not retire: "..phase)
end
local function runCase(clients,pins,center,rank)
    local source,primary,ally=clients[1],clients[2],clients[3]
    local phase="rank"..rank
    waitClean(source,pins[source.ID],phase.."-initial-clean",2500)
    for _,client in ipairs(clients) do restore(client);cooldown(client) end
    rankFixture(source,pins[source.ID],rank)
    local primaryBefore,allyBefore=read(primary,pins[primary.ID]),read(ally,pins[ally.ID])
    record({event="case_start",phase=phase,server_fixture_rank=rank,is_native_rank_selection=false,
        center=center,flak_center=offset(center,2,-1),hostile_origin=primaryBefore.native.position,
        friendly_origin=allyBefore.native.position,hostile_aim=offset(center,-15,-2),friendly_aim=offset(center,-13,0),
        nominal_projectile_speed=15,nominal_projectile_range=20})
    check(scenario.Parallel({
        hostile=function() cast(primary,pins[primary.ID],offset(center,-15,-2),605,phase.."-hostile-FireWave");return nil end,
        friendly=function() cast(ally,pins[ally.ID],offset(center,-13,0),605,phase.."-friendly-FireWave");return nil end,
    }))
    local deadline=scenario.Time()+1800
    local launched
    while scenario.Time()<deadline do
        local observed=observe(source,pins[source.ID],phase.."-launch")
        local primaryNow,allyNow=read(primary,pins[primary.ID]),read(ally,pins[ally.ID])
        if observed.world.arena.friendly_projectile_count>=1 and observed.world.arena.hostile_projectile_count>=1
            and primaryNow.world.hero.mana_point<primaryBefore.world.hero.mana_point
            and allyNow.world.hero.mana_point<allyBefore.world.hero.mana_point then launched=observed;break end
        pause(20)
    end
    if not launched then error("both native projectile launches and power admission unobserved; no replay") end
    -- Publication is real; this bounded travel interval puts the crossing flight
    -- inside the authored circle before the ordinary native Flak input completes.
    pause(100)
    local sourceBefore=read(source,pins[source.ID])
    cast(source,pins[source.ID],offset(center,2,-1),640,phase.."-intercept")
    deadline=scenario.Time()+1800
    local intercepted
    while scenario.Time()<deadline do
        local observed=observe(source,pins[source.ID],phase.."-intercept")
        local hero,arena=observed.world.hero,observed.world.arena
        if hero.flak_stack_count>0 and arena.hostile_projectile_count==0
            and arena.friendly_projectile_count>0 and hero.mana_point<sourceBefore.world.hero.mana_point then
            intercepted=observed;break
        end
        pause(20)
    end
    if not intercepted then error("hostile deletion/Flak gain with friendly flight survival unobserved; no replay") end
    local perStack=rank==7 and .05 or .12
    if math.abs(intercepted.world.hero.flak_attack_speed-intercepted.world.hero.flak_stack_count*perStack)>.001 then
        error("real Flak attack speed does not match observed stack count")
    end
    record({event="intercept_verified",phase=phase,server_fixture_rank=rank,
        flak_stack_count=intercepted.world.hero.flak_stack_count,flak_attack_speed=intercepted.world.hero.flak_attack_speed,
        friendly_projectile_count=intercepted.world.arena.friendly_projectile_count,
        hostile_projectile_count=intercepted.world.arena.hostile_projectile_count,
        elapsed_since_both_publication_ms=intercepted.at_ms-launched.at_ms,
        is_published_projectile_destruction_observed=true,is_friendly_flight_survival_observed=true,
        is_native_rank_selection=false,is_projectile_render_positions_measured=false})
    if rank==7 then
        deadline=intercepted.at_ms+11500
        local expired
        while scenario.Time()<deadline do
            local observed=observe(source,pins[source.ID],"rank7-natural-expiry")
            if observed.world.hero.flak_stack_count==0 and math.abs(observed.world.hero.flak_attack_speed)<.001 then expired=observed;break end
            pause(200)
        end
        if not expired or expired.at_ms-intercepted.at_ms<9400 then error("Flak natural10s expiry unobserved or early") end
        record({event="buff_natural_expiry",phase=phase,elapsed_ms=expired.at_ms-intercepted.at_ms})
    else
        local clean=waitClean(source,pins[source.ID],"rank8-before-empty-recast",3000)
        if clean.world.hero.flak_stack_count==0 then error("Flak expired before empty recast; inconclusive") end
        restore(source);cooldown(source)
        local beforeEmpty=read(source,pins[source.ID])
        cast(source,pins[source.ID],offset(center,2,-1),640,"rank8-empty-recast")
        deadline=scenario.Time()+1800
        local cleared
        while scenario.Time()<deadline do
            local observed=observe(source,pins[source.ID],"rank8-empty-recast")
            if observed.world.hero.flak_stack_count==0 and math.abs(observed.world.hero.flak_attack_speed)<.001
                and observed.world.hero.mana_point<beforeEmpty.world.hero.mana_point then cleared=observed;break end
            pause(25)
        end
        if not cleared then error("zero-projectile recast did not remove old Flak buff") end
        record({event="zero_projectile_recast_cleared",phase=phase,
            power_spent=beforeEmpty.world.hero.mana_point-cleared.world.hero.mana_point})
    end
end
return function()
    check(scenario.RequireAPI(1))
    local source,primary=must(scenario.GetClient(sourceID)),must(scenario.GetClient(targetID))
    local ally,splash=must(scenario.GetClient(allyID)),must(scenario.GetClient(splashID))
    local clients={source,primary,ally,splash}
    local pins,pids,instances={},{},{}
    local expectedNouns={2038499613,700180862,700180862,700180862}
    for index,client in ipairs(clients) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then error("Flak follow-up requires1280x720") end
        if pids[client.PID] or instances[client.InstanceID] then error("owned identity collision") end
        pids[client.PID],instances[client.InstanceID]=true,true
        pins[client.ID]=read(client)
        if pins[client.ID].world.hero.index~=0 or pins[client.ID].world.hero.noun~=expectedNouns[index]
            or pins[client.ID].world.game_id~=pins[source.ID].world.game_id then error("retained Flak2v2 actor mismatch") end
        rankFixture(client,pins[client.ID],0)
        actor(client,pins[client.ID],"initial",0)
    end
    local center=read(source,pins[source.ID]).native.position
    place(ally,pins[ally.ID],offset(center,7,0));place(primary,pins[primary.ID],offset(center,5,-2))
    place(splash,pins[splash.ID],offset(center,5,0))
    runCase(clients,pins,center,7)
    runCase(clients,pins,center,8)
    rankFixture(source,pins[source.ID],0)
    waitClean(source,pins[source.ID],"final-projectiles",3000)
    local befores={}
    for _,client in ipairs(clients) do befores[client.ID]=read(client,pins[client.ID]) end
    pause(100)
    for _,client in ipairs(clients) do
        local final=actor(client,pins[client.ID],"final",0)
        if final.native.frame_sequence<=befores[client.ID].native.frame_sequence then error("final native frame stalled") end
        local capture=must(client.Diagnostics:Capture("flak-projectile-final"))
        record({event="diagnostic",client_id=client.ID,manifest=capture.manifest_path})
    end
    must(source.UI:Screenshot("flak-projectile-source"));must(primary.UI:Screenshot("flak-projectile-primary"))
    record({event="suite_end",passed=2,is_four_clients_live=true,is_hostile_destruction_and_friendly_survival_observed=true,
        is_flak_stack_and_speed_verified=true,is_natural_expiry_and_empty_recast_verified=true,
        is_native_rank_selection_verified=false,is_full_ability_parity_verified=false})
end
