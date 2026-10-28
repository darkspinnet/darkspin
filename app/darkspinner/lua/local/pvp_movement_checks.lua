---Focused build-103 PvP Quantum Blink immunity and Ride the Lightning kill-reset probes.
---Uses one owned duel on the configured scenario server. Retain both native traces.
local must,check=scenario.Must,scenario.Check
local sourceID,targetID="pvp-movement-check-a","pvp-movement-check-b"
local function pause(ms) check(scenario.Sleep(ms)) end
local function record(kind,fields)
    local parts={"movement_check "..kind,"at="..math.floor(scenario.Time())}
    for key,item in pairs(fields or {}) do parts[#parts+1]=key.."="..tostring(item) end
    table.sort(parts)
    check(scenario.Log(table.concat(parts," ")))
end
local function distance(first,second)
    local dx,dy=first.x-second.x,first.y-second.y
    return math.sqrt(dx*dx+dy*dy)
end
local function read(client)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if probe.pid~=client.PID or probe.instance_id~=client.InstanceID or not world.arena
        or not world.arena.is_combat_active or #world.arena.members~=2 or not world.hero
        or not native or not native.is_world_active or not native.position then
        error("owned living Arena/native producer unavailable")
    end
    return {world=world,native=native,at=scenario.Time()}
end
local function sameActor(first,second)
    return first.world.game_id==second.world.game_id
        and first.world.peer_generation==second.world.peer_generation
        and first.world.transport_generation==second.world.transport_generation
        and first.world.hero.object_id==second.world.hero.object_id
        and first.world.hero.index==second.world.hero.index
end
local function member(observed)
    for _,entry in ipairs(observed.world.arena.members) do
        if entry.user_id==observed.world.user_id then return entry end
    end
    error("owned Arena member absent")
end
local function click(client,point)
    local receipt=must(client.World:Click(point))
    if not receipt.is_processed then error("native click input not processed; no replay") end
end
local function hold(client,point,duration)
    local receipt=must(client.World:Hold(point,duration))
    local poll=receipt.gameplay_click
    if not receipt.is_processed or not poll or not poll.is_press_poll_observed
        or not poll.is_release_poll_observed then error("native hold lacks paired polls; no replay") end
end
local function stop(client) click(client,read(client).native.position) end
local function ensureHero(client,noun)
    local observed=read(client)
    if observed.world.hero.index~=0 then
        must(client.World:ResetCooldowns())
        must(client.World:SelectHero(0,20000))
        observed=read(client)
    end
    if observed.world.hero.noun~=noun then
        local receipt=must(client.World:ReplaceHero(noun))
        if not receipt.is_replacement or not receipt.is_rig_ready or receipt.recipient_count~=2 then
            error("native replacement was not established on both clients")
        end
    end
    return read(client)
end
local function restore(client,req)
    local before=read(client)
    local receipt=must(client.World:RestoreHero(req or {}))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("resource fixture was not shared") end
    local expectedMaximum=req and req.maximum_hit_point or before.world.hero.maximum_hit_point
    local expectedHP=req and req.hit_point or expectedMaximum
    local deadline=scenario.Time()+4000
    while scenario.Time()<deadline do
        local after=read(client)
        if math.abs(after.world.hero.maximum_hit_point-expectedMaximum)<.1
            and math.abs(after.world.hero.hit_point-expectedHP)<.1 then return end
        pause(50)
    end
    error("resource fixture did not settle on owned actor")
end
local function reset(client)
    local receipt=must(client.World:ResetCooldowns())
    if not receipt.is_cooldown_reset or receipt.recipient_count~=2 then error("cooldown fixture was not shared") end
end
local function place(client,point)
    local before=read(client)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("placement fixture was not shared") end
    local deadline=scenario.Time()+7000
    while scenario.Time()<deadline do
        local after=read(client)
        if sameActor(before,after) and after.native.frame_sequence>before.native.frame_sequence
            and distance(after.native.position,point)<.5 then return after end
        pause(60)
    end
    error("placed native hero did not settle")
end
local function cast(client,point)
    local button=must(client.UI:Click(575,655))
    if not button.is_processed then error("native special-two button unprocessed; no replay") end
    pause(150)
    click(client,{x=point.x,y=point.y,z=point.z+1.5})
end
local function missing(failure)
    local cause=failure
    while cause and (not cause.Code or cause.Code=="") do cause=cause.Cause end
    return cause and cause.Code=="not_found"
end
local function launch(server,id,noun)
    local profile=must(server:CreateProfile({name=id,
        hero_nouns={noun,117394854,3068979135},chain_progression=2,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=id,launch_mode="detached"}))
    must(client:Login());must(client.Scene:Wait(scene.Arsenal))
    return client
end
local function ownedPair(server)
    local source,sourceError=scenario.GetClient(sourceID)
    local target,targetError=scenario.GetClient(targetID)
    if source and target then
        if read(source).world.game_id~=read(target).world.game_id then error("owned duel game mismatch") end
        return source,target
    end
    if source or target or not missing(sourceError) or not missing(targetError) then
        error("partial owned duel; no setup replay")
    end
    source=launch(server,sourceID,1240682488)
    target=launch(server,targetID,454054015)
    must(source.Party:Create("movement-check"))
    must(source.Party:Invite(target));must(target.Party:Join(source))
    must(source.Scene:Set(scene.Navigation));must(target.Scene:Set(scene.Navigation))
    must(source:WaitForReportedLocation("navigation",15000))
    must(target:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{target}}}))
    check(scenario.Parallel({source=function() must(source.PVP:Accept());return nil end,
        target=function() must(target.PVP:Accept());return nil end}))
    must(source.PVP:Wait());must(target.PVP:Wait())
    return source,target
end
local function quantumImmunity(source,target)
    ensureHero(source,1240682488);ensureHero(target,454054015)
    stop(source);stop(target)
    restore(source);restore(target,{maximum_hit_point=10000})
    reset(source);reset(target)
    local anchor=read(target).native.position
    place(source,{x=anchor.x-3,y=anchor.y,z=anchor.z})
    local beforeSource,beforeTarget=read(source),read(target)
    if beforeTarget.world.hero.hit_point~=10000 then error("nonlethal Blink fixture not observed") end
    local attackStart,attackEnd,attackDispatch
    record("quantum_before",{source_hp=beforeSource.world.hero.hit_point,
        target_hp=beforeTarget.world.hero.hit_point,source_pid=source.PID,target_pid=target.PID})
    local dispatchBefore=read(target).world.ability.last_dispatch
    local sourceDispatchBefore=beforeSource.world.ability.last_dispatch
    check(scenario.Parallel({
        cast=function() cast(source,beforeTarget.native.position);return nil end,
        attack=function()
            local deadline=scenario.Time()+3000
            local dispatch
            while scenario.Time()<deadline do
                dispatch=read(source).world.ability.last_dispatch
                if dispatch and dispatch.ability_asset=="QuantumBlink"
                    and (not sourceDispatchBefore or dispatch.revision>sourceDispatchBefore.revision) then break end
                pause(15)
            end
            if not dispatch or dispatch.ability_asset~="QuantumBlink"
                or (sourceDispatchBefore and dispatch.revision<=sourceDispatchBefore.revision) then
                error("Blink dispatch was not observed before attack")
            end
            pause(250)
            local observed=read(source)
            attackStart=scenario.Time()
            hold(target,observed.native.position,550)
            attackEnd=scenario.Time()
            attackDispatch=read(target).world.ability.last_dispatch
            return nil
        end,
    }))
    record("quantum_attack",{start=attackStart,finish=attackEnd,
        attack_asset=attackDispatch and attackDispatch.ability_asset})
    if not attackDispatch or attackDispatch.ability_asset~="LightningRogueBasic"
        or (dispatchBefore and attackDispatch.revision<=dispatchBefore.revision) then
        error("opponent basic attack was not dispatched during Blink")
    end
    local during=read(source)
    if not sameActor(beforeSource,during) then error("Blink source actor changed") end
    record("quantum_during",{hp=during.world.hero.hit_point,
        attack_damage_done=member(read(target)).damage_done-member(beforeTarget).damage_done})
    if during.world.hero.hit_point~=beforeSource.world.hero.hit_point then
        error("Blink source took damage during the attempted immunity window")
    end
    pause(2300)
    local released=read(source)
    if released.world.hero.hit_point~=beforeSource.world.hero.hit_point then
        error("Blink source lost HP before post-cast damaging control")
    end
    stop(target);reset(target)
    local baselineBefore=read(source)
    local targetBefore=read(target)
    hold(target,baselineBefore.native.position,800)
    local baselineAfter=read(source)
    local targetAfter=read(target)
    record("quantum_baseline",{before_hp=baselineBefore.world.hero.hit_point,
        after_hp=baselineAfter.world.hero.hit_point,
        target_damage=member(targetAfter).damage_done-member(targetBefore).damage_done})
    if not sameActor(baselineBefore,baselineAfter)
        or baselineAfter.world.hero.hit_point>=baselineBefore.world.hero.hit_point then
        error("post-Blink basic attack did not establish a damaging control")
    end
    record("quantum_result",{is_immunity_observed=true,
        blink_damage=member(read(source)).damage_done-member(beforeSource).damage_done})
end
local function rideKillReset(source,target)
    ensureHero(source,454054015);ensureHero(target,1240682488)
    stop(source);stop(target)
    restore(source);restore(target,{hit_point=1})
    reset(source);reset(target)
    local targetBefore=read(target)
    place(source,{x=targetBefore.native.position.x-3,y=targetBefore.native.position.y,
        z=targetBefore.native.position.z})
    local sourceBefore=read(source)
    record("ride_before",{source_pid=source.PID,target_pid=target.PID,
        source_object=sourceBefore.world.hero.object_id,target_object=targetBefore.world.hero.object_id,
        target_hp=targetBefore.world.hero.hit_point})
    if targetBefore.world.hero.hit_point~=1 then error("Ride target kill fixture not observed") end
    local firstStarted=scenario.Time()
    cast(source,targetBefore.native.position)
    local deadline=scenario.Time()+3000
    local targetAfter
    while scenario.Time()<deadline do
        targetAfter=read(target)
        if targetAfter.world.hero.object_id~=targetBefore.world.hero.object_id then break end
        pause(20)
    end
    if not targetAfter or targetAfter.world.hero.object_id==targetBefore.world.hero.object_id then
        error("Ride target defeat and squad transition unobserved")
    end
    local sourceAfter=read(source)
    record("ride_kill",{target_object=targetAfter.world.hero.object_id,
        target_index=targetAfter.world.hero.index,
        damage_taken=member(targetAfter).damage_taken-member(targetBefore).damage_taken})
    if not sameActor(sourceBefore,sourceAfter) then
        error("Ride source actor changed after target defeat")
    end
    local rigDeadline=scenario.Time()+6000
    while scenario.Time()<rigDeadline and not read(target).native.is_rig_ready do pause(50) end
    restore(target,{maximum_hit_point=10000})
    local nextTarget=read(target)
    place(source,{x=nextTarget.native.position.x-3,y=nextTarget.native.position.y,
        z=nextTarget.native.position.z})
    local secondStarted=scenario.Time()
    local dispatchBefore=read(source).world.ability.last_dispatch
    cast(source,read(target).native.position)
    deadline=scenario.Time()+2500
    local dispatch
    while scenario.Time()<deadline do
        local observed=read(source)
        dispatch=observed.world.ability and observed.world.ability.last_dispatch
        if dispatch and (not dispatchBefore or dispatch.revision>dispatchBefore.revision)
            and dispatch.ability_asset=="LightningRogueActive" then break end
        pause(30)
    end
    record("ride_recast",{first_started=firstStarted,second_started=secondStarted,
        interval_ms=secondStarted-firstStarted,revision=dispatch and dispatch.revision,
        asset=dispatch and dispatch.ability_asset})
    if not dispatch or dispatch.ability_asset~="LightningRogueActive"
        or (dispatchBefore and dispatch.revision<=dispatchBefore.revision)
        or secondStarted-firstStarted>=10000 then
        error("Ride native recast before authored 10s cooldown was not verified")
    end
    record("ride_result",{is_kill_cooldown_reset_observed=true})
end
return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local source,target=ownedPair(server)
    for _,client in ipairs({source,target}) do
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then
            error("native special-two coordinates require 1280x720 windows")
        end
    end
    quantumImmunity(source,target)
    rideKillReset(source,target)
    local first,second=read(source),read(target)
    pause(100)
    local firstAfter,secondAfter=read(source),read(target)
    if firstAfter.native.frame_sequence<=first.native.frame_sequence
        or secondAfter.native.frame_sequence<=second.native.frame_sequence then
        error("final owned native frames stalled")
    end
    record("suite_end",{source_pid=source.PID,target_pid=target.PID,is_complete=true})
end
