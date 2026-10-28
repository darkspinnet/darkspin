---One owned build-103 Arena duel for healing, enrage and resurrection branches.
---Run on the configured single scenario server after other scenarios finish.
local must, check = scenario.Must, scenario.Check
local spriteNoun, fireNoun, healerNoun, beastNoun = 647027626, 709733043, 749013658, 1264710567
local selectedBranches = {sprite=true,fire=true,briars=true,beast=true}

local function pause(ms) check(scenario.Sleep(ms)) end

local function record(label, source, target, detail)
    local first, second = must(source.World:SnapshotLiving()), must(target.World:SnapshotLiving())
    local sourceNative = must(source.World:SnapshotNative()).gameplay_state
    local targetNative = must(target.World:SnapshotNative()).gameplay_state
    check(scenario.Log(label.." detail="..tostring(detail).." source_pid="..source.PID
        .." target_pid="..target.PID.." source_hp="..first.hero.hit_point
        .." source_max="..first.hero.maximum_hit_point.." target_hp="..second.hero.hit_point
        .." source_frame="..sourceNative.frame_sequence.." target_frame="..targetNative.frame_sequence))
end

local function world(client)
    local state = must(client.World:SnapshotLiving())
    if not state.arena or not state.arena.is_combat_active or not state.hero then
        error("owned Arena hero unavailable")
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
        hero_nouns={117394854,3068979135,healerNoun}, chain_progression=2,is_pvp_enabled=true}))
    local client = must(profile:Launch({client_id=name,launch_mode="detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client,profile
end

local function replace(client,noun)
    local before = native(client)
    local receipt = must(client.World:ReplaceHero(noun))
    if not receipt.is_replacement or not receipt.is_queued or receipt.recipient_count~=2 then
        error("Arena replacement not queued to both duel clients")
    end
    local deadline, previous, stable = scenario.Time()+9000,nil,0
    while scenario.Time()<deadline do
        pause(100)
        local current = native(client)
        if world(client).hero.noun==noun and current.is_rig_observed and current.is_rig_ready
            and current.controlled_handle~=before.controlled_handle and previous
            and current.controlled_handle==previous.controlled_handle
            and current.owner_thread_id==previous.owner_thread_id
            and current.frame_sequence>previous.frame_sequence then
            stable=stable+1
            if stable>=2 then return end
        else
            stable=0
        end
        previous=current
    end
    error("Arena replacement native rig did not settle")
end

local function reset(client)
    local receipt = must(client.World:ResetCooldowns())
    if not receipt.is_queued or not receipt.is_cooldown_reset or receipt.recipient_count~=2 then
        error("cooldown reset not queued to both duel clients")
    end
end

local function rank(client,requested)
    local before=world(client)
    local receipt=must(client.World:SetAbilityRank(requested))
    if not receipt.is_ability_rank_set or not receipt.is_server_rank_fixture or receipt.is_queued
        or receipt.ability_rank~=requested or receipt.object_id~=before.hero.object_id
        or receipt.recipient_count~=2 then
        error("Arena server rank fixture not established")
    end
end

local function restore(client,hp,maxHP)
    local request = {}
    if hp then request.hit_point=hp end
    if maxHP then request.maximum_hit_point=maxHP end
    local receipt = must(client.World:RestoreHero(request))
    if not receipt.is_queued or receipt.recipient_count~=2 then
        error("resource fixture not queued to both duel clients")
    end
    local deadline=scenario.Time()+4000
    while scenario.Time()<deadline do
        local hero=world(client).hero
        if (not hp or math.abs(hero.hit_point-hp)<0.1)
            and (not maxHP or math.abs(hero.maximum_hit_point-maxHP)<0.1) then return end
        pause(100)
    end
    error("resource fixture not observed")
end

local function placeNear(source,target,distance)
    local position=native(target).position
    local point={x=position.x-distance,y=position.y,z=position.z}
    local before=native(source)
    local receipt=must(source.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then
        error("Arena placement not queued to both duel clients")
    end
    local deadline=scenario.Time()+5000
    while scenario.Time()<deadline do
        local current=native(source)
        local dx,dy=current.position.x-point.x,current.position.y-point.y
        if dx*dx+dy*dy<0.25 and current.frame_sequence>before.frame_sequence then return end
        pause(100)
    end
    error("Arena placement not observed")
end

local function cast(source,target,asset,button,isSelf)
    local before=world(source)
    local previous=before.ability and before.ability.last_dispatch
    local revision=previous and previous.revision or 0
    local position=native(isSelf and source or target).position
    if not isSelf then position={x=position.x,y=position.y,z=position.z+1.5} end
    local edge=must(source.UI:Click(button,655))
    if not edge.is_processed then error("ability hotbar edge unprocessed") end
    pause(150)
    local click=must(source.World:Click(position))
    if not click.is_processed then error("ability world edge unprocessed") end
    local deadline=scenario.Time()+5000
    while scenario.Time()<deadline do
        local current=world(source)
        local dispatch=current.ability and current.ability.last_dispatch
        if dispatch and dispatch.revision>revision then
            if dispatch.actor_object_id~=before.hero.object_id or dispatch.ability_asset~=asset then
                error("ability dispatch identity mismatch")
            end
            return dispatch
        end
        pause(100)
    end
    error("accepted ability dispatch unavailable; no replay")
end

local function capture(source,target,label)
    local first=must(source.Diagnostics:Capture(label.."-caster"))
    local second=must(target.Diagnostics:Capture(label.."-observer"))
    check(scenario.Log(label.." caster_manifest="..first.manifest_path
        .." observer_manifest="..second.manifest_path))
end

local function waitHealing(source,before,deadlineMS)
    local deadline=scenario.Time()+deadlineMS
    while scenario.Time()<deadline do
        local hero=world(source).hero
        if hero.hit_point>before.hit_point and hero.hit_point<=hero.maximum_hit_point then
            return hero.hit_point-before.hit_point
        end
        pause(100)
    end
    error("injured Arena hero received no observed healing")
end

local function sprite(source,target)
    replace(source,spriteNoun)
    placeNear(source,target,3)
    local maximum=world(source).hero.maximum_hit_point
    restore(source,maximum*0.4)
    reset(source)
    local before=world(source).hero
    record("sprite_before",source,target,"wounded")
    cast(source,target,"SummonSprite",605,true)
    local amount=waitHealing(source,before,8000)
    record("sprite_healed",source,target,amount)
    capture(source,target,"sprite-healed")
end

local function fire(source,target)
    replace(source,fireNoun)
    placeNear(source,target,2)
    restore(target,10000,10000)
    reset(source)
    cast(source,target,"FireTempestActive",575,true)
    pause(2500)
    record("fire_pet_before_enrage",source,target,"summoned")
    reset(source)
    cast(source,target,"FireTempestActive",575,true)
    pause(5500)
    record("fire_pet_enraged",source,target,"recast")
    capture(source,target,"fire-pet-enraged")
end

local function briars(source,target)
    replace(source,healerNoun)
    -- Build-103 Strangling Briars first heals at rank 5; rank 1 deals damage only.
    rank(source,5)
    placeNear(source,target,2)
    restore(target,10000,10000)
    local maximum=world(source).hero.maximum_hit_point
    restore(source,maximum*0.4)
    reset(source)
    local before=world(source).hero
    record("briars_before",source,target,"wounded")
    cast(source,target,"SupportHealerSupport",640,false)
    local amount=waitHealing(source,before,10000)
    record("briars_healed",source,target,amount)
    capture(source,target,"briars-healed")
end

local function killForBeast(source,target)
    local aim=native(source).position
    local ownerID=world(source).hero.object_id
    for attempt=1,6 do
        local current=must(source.World:Snapshot())
        if not current.hero then error("Savage Ally owner unavailable before lethal input") end
        if current.hero and current.hero.object_id~=ownerID then
            error("Savage Ally owner changed before resurrection")
        end
        if current.hero and current.hero.hit_point<=0 then return end
        restore(source,1)
        reset(target)
        local receipt=must(target.World:Hold({x=aim.x,y=aim.y,z=aim.z+1.5},550))
        local click=receipt.gameplay_click
        if not receipt.is_processed or not click or not click.is_press_poll_observed
            or not click.is_release_poll_observed then
            error("lethal basic input lacks paired native polls; no replay")
        end
        local deadline=scenario.Time()+1500
        while scenario.Time()<deadline do
            current=must(source.World:Snapshot())
            if not current.hero then return end
            if current.hero and current.hero.object_id~=ownerID then
                error("Savage Ally owner switched after lethal input")
            end
            if current.hero and current.hero.hit_point<=0 then return end
            pause(100)
        end
        record("beast_defended_attempt",source,target,attempt)
    end
    error("six distinct lethal basic attempts failed to defeat wounded hero")
end

local function beast(source,target)
    replace(source,beastNoun)
    local ownerID=world(source).hero.object_id
    placeNear(target,source,1)
    restore(target,10000,10000)
    reset(source)
    cast(source,target,"SummonBeast",640,true)
    pause(2000)
    capture(source,target,"beast-before-lethal")
    killForBeast(source,target)
    local deadline=scenario.Time()+7000
    while scenario.Time()<deadline do
        local state=must(source.World:Snapshot())
        if state.hero and state.hero.object_id~=ownerID then
            error("Savage Ally owner switched instead of being resurrected")
        end
        if state.hero and state.hero.hit_point>0 and state.arena and state.arena.is_combat_active then
            record("beast_resurrected",source,target,state.hero.hit_point)
            capture(source,target,"beast-resurrected")
            return
        end
        pause(100)
    end
    capture(source,target,"beast-no-resurrection")
    error("Arena Beast did not resurrect its defeated owner")
end

return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local source,sourceProfile=launch(server,"pvp-companion-caster")
    local target,targetProfile=launch(server,"pvp-companion-observer")
    must(source.Party:Create("pvp-companion-branches"))
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
    if selectedBranches.sprite then sprite(source,target) end
    if selectedBranches.fire then fire(source,target) end
    if selectedBranches.briars then briars(source,target) end
    if selectedBranches.beast then beast(source,target) end
end
