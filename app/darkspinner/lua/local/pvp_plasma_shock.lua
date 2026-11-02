---Native Arena Plasma Sentinel Basic shock-proc capture on one owned duel.
---Run after any other scenario ends. StartServer uses the one configured port.
local must, check = scenario.Must, scenario.Check
local plasmaNoun = 576185321
local maximumAttempts = 40

local function pause(ms) check(scenario.Sleep(ms)) end

local function world(client)
    local state = must(client.World:SnapshotLiving())
    if not state.arena or not state.arena.is_combat_active or not state.hero then
        error("active owned Arena hero unavailable")
    end
    return state
end

local function native(client)
    local state = must(client.World:SnapshotNative()).gameplay_state
    if not state or not state.is_world_active or not state.position then
        error("owned native rig unavailable")
    end
    return state
end

local function launch(server, name)
    local profile = must(server:CreateProfile({name=name,
        hero_nouns={117394854,3068979135,plasmaNoun},
        chain_progression=2,is_pvp_enabled=true}))
    local client = must(profile:Launch({client_id=name,launch_mode="detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client, profile
end

local function replaceSource(client)
    local before = native(client)
    local receipt = must(client.World:ReplaceHero(plasmaNoun))
    if not receipt.is_replacement or not receipt.is_queued or receipt.recipient_count~=2 then
        error("Plasma Sentinel replacement not queued to both duel clients")
    end
    local deadline = scenario.Time()+8000
    local previous, stableCount = nil, 0
    while scenario.Time()<deadline do
        pause(100)
        local current = native(client)
        local hero = world(client).hero
        if hero.noun==plasmaNoun and current.is_rig_observed and current.is_rig_ready
            and current.controlled_handle~=before.controlled_handle
            and previous and current.controlled_handle==previous.controlled_handle
            and current.owner_thread_id==previous.owner_thread_id
            and current.frame_sequence>previous.frame_sequence then
            stableCount=stableCount+1
            if stableCount>=2 then return end
        else
            stableCount=0
        end
        previous=current
    end
    error("Plasma Sentinel native replacement did not settle")
end

local function placeNear(source, target)
    local position = native(target).position
    local point = {x=position.x-1,y=position.y,z=position.z}
    local before = native(source)
    local receipt = must(source.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then
        error("melee fixture placement not published to both duel clients")
    end
    local deadline = scenario.Time()+8000
    while scenario.Time()<deadline do
        local current = native(source)
        local dx,dy=current.position.x-point.x,current.position.y-point.y
        if dx*dx+dy*dy<0.25 and current.frame_sequence>before.frame_sequence then return end
        pause(100)
    end
    error("Plasma Sentinel native melee placement not observed")
end

local function restoreTarget(target)
    local receipt = must(target.World:RestoreHero({maximum_hit_point=10000,hit_point=10000}))
    if not receipt.is_queued or receipt.recipient_count~=2 then
        error("target HP fixture not published to both duel clients")
    end
    local deadline = scenario.Time()+4000
    while scenario.Time()<deadline do
        local hero = world(target).hero
        if math.abs(hero.hit_point-10000)<0.1 and math.abs(hero.maximum_hit_point-10000)<0.1 then return end
        pause(50)
    end
    error("target HP fixture not observed")
end

local function capture(source,target,label)
    local first = must(source.Diagnostics:Capture(label.."-caster"))
    local second = must(target.Diagnostics:Capture(label.."-recipient"))
    check(scenario.Log(label.." caster_manifest="..first.manifest_path
        .." recipient_manifest="..second.manifest_path))
end

local function castOnce(source,target,attempt)
    local beforeSource,beforeTarget=world(source),world(target)
    local beforeDispatch=beforeSource.ability and beforeSource.ability.last_dispatch
    local revision=beforeDispatch and beforeDispatch.revision or 0
    local sourceNative,targetNative=native(source),native(target)
    if not sourceNative.is_rig_observed or not sourceNative.is_rig_ready
        or not targetNative.is_rig_observed or not targetNative.is_rig_ready then
        error("both owned native rigs must be ready before Plasma input")
    end
    local aim=targetNative.position
    local receipt=must(source.World:Hold({x=aim.x,y=aim.y,z=aim.z+1.5},450))
    local click=receipt.gameplay_click
    if not receipt.is_processed or not click or not click.is_press_poll_observed
        or not click.is_release_poll_observed then
        error("Plasma basic input lacks paired native polls; no ambiguous replay")
    end
    local deadline=scenario.Time()+1100
    local observed
    while scenario.Time()<deadline do
        local sourceWorld,targetWorld=world(source),world(target)
        if sourceWorld.game_id~=beforeSource.game_id or targetWorld.game_id~=beforeTarget.game_id
            or sourceWorld.peer_generation~=beforeSource.peer_generation
            or targetWorld.peer_generation~=beforeTarget.peer_generation
            or sourceWorld.transport_generation~=beforeSource.transport_generation
            or targetWorld.transport_generation~=beforeTarget.transport_generation
            or sourceWorld.hero.object_id~=beforeSource.hero.object_id
            or targetWorld.hero.object_id~=beforeTarget.hero.object_id then
            error("Plasma attempt changed the pinned Arena actors")
        end
        local dispatch=sourceWorld.ability and sourceWorld.ability.last_dispatch
        if dispatch and dispatch.revision>revision then
            if dispatch.actor_object_id~=beforeSource.hero.object_id
                or dispatch.ability_asset~="PlasmaSentinelBasic" then
                error("Plasma dispatch identity mismatch; no replay")
            end
            observed=dispatch
        end
        if observed and targetWorld.hero.is_stunned then
            check(scenario.Log("plasma_shock_proc attempt="..attempt
                .." revision="..observed.revision
                .." caster_pid="..source.PID.." recipient_pid="..target.PID
                .." caster_frame="..native(source).frame_sequence
                .." recipient_frame="..native(target).frame_sequence
                .." target_hp="..targetWorld.hero.hit_point))
            return true
        end
        pause(40)
    end
    if not observed then error("Plasma basic dispatch not observed; no uncertain replay") end
    check(scenario.Log("plasma_no_shock attempt="..attempt.." revision="..observed.revision
        .." target_hp="..world(target).hero.hit_point
        .." caster_frame="..native(source).frame_sequence
        .." recipient_frame="..native(target).frame_sequence))
    return false
end

local function run()
    local server=must(scenario.StartServer({}))
    local source,sourceProfile=launch(server,"plasma-shock-caster")
    local target,targetProfile=launch(server,"plasma-shock-recipient")
    must(source.Party:Create("plasma-shock"))
    must(source.Party:Invite(target))
    must(target.Party:Join(source))
    must(source.Scene:Set(scene.Navigation))
    must(target.Scene:Set(scene.Navigation))
    must(sourceProfile:WaitForReportedLocation("navigation",15000))
    must(targetProfile:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{target}}}))
    must(source.PVP:Accept())
    must(target.PVP:Accept())
    must(source.PVP:Wait())
    must(target.PVP:Wait())
    replaceSource(source)
    placeNear(source,target)
    capture(source,target,"plasma-shock-before")
    for attempt=1,maximumAttempts do
        restoreTarget(target)
        local reset = must(source.World:ResetCooldowns())
        if not reset.is_queued or not reset.is_cooldown_reset or reset.recipient_count~=2 then
            error("Plasma cooldown reset not queued to both duel clients")
        end
        if castOnce(source,target,attempt) then
            capture(source,target,"plasma-shock-created")
            pause(1250)
            if world(target).hero.is_stunned then
                error("Plasma shock remained active after its authored one-second duration")
            end
            capture(source,target,"plasma-shock-expired")
            check(scenario.Log("plasma_shock_complete attempt="..attempt
                .." inspect both manifests for PlasmaSentinelShock 0xA2/A4 and stun attributes"))
            return
        end
    end
    capture(source,target,"plasma-shock-no-proc")
    error("no Plasma Sentinel shock proc in "..maximumAttempts.." verified native attempts")
end

return function()
    check(scenario.RequireAPI(1))
    run()
end
