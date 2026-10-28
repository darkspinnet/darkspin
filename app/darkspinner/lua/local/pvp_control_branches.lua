---Focused build-103 Arena control branches in one four-player match.
---Run on the configured single scenario server; inspect all four client traces.
local must,check=scenario.Must,scenario.Check
local ids={"control-1a","control-1b","control-2a","control-2b"}
local bioNoun,plagueNoun,spaceNoun,missileNoun=308338531,1237326306,507464763,3524487169

local function pause(ms) check(scenario.Sleep(ms)) end
local function record(label,items)
    local parts={"control_branch",label,"at="..math.floor(scenario.Time())}
    for key,item in pairs(items or {}) do parts[#parts+1]=key.."="..tostring(item) end
    table.sort(parts)
    check(scenario.Log(table.concat(parts," ")))
end
local function read(client)
    local world=must(client.World:SnapshotLiving())
    local native=must(client.World:SnapshotNative()).gameplay_state
    if not world.arena or not world.arena.is_combat_active or #world.arena.members~=4
        or not world.hero or not native or not native.is_world_active or not native.position then
        error("four-player owned Arena/native state unavailable")
    end
    return {world=world,native=native}
end
local function frame(client) return read(client).native.frame_sequence end
local function hero(client) return read(client).world.hero end
local function point(client) return read(client).native.position end
local function click(client,position)
    local receipt=must(client.World:Click(position))
    if not receipt.is_processed then error("native world click unprocessed; no replay") end
end
local function hold(client,position)
    local receipt=must(client.World:Hold(position,600))
    local poll=receipt.gameplay_click
    if not receipt.is_processed or not poll or not poll.is_press_poll_observed
        or not poll.is_release_poll_observed then
        error("native basic press/release uncertain; no replay")
    end
end
local function stop(client) click(client,point(client)) end
local function reset(client)
    local receipt=must(client.World:ResetCooldowns())
    if not receipt.is_cooldown_reset or receipt.recipient_count~=4 then
        error("four-player cooldown reset not published")
    end
end
local function restore(client)
    local receipt=must(client.World:RestoreHero({maximum_hit_point=10000,hit_point=10000}))
    if not receipt.is_queued or receipt.recipient_count~=4 then
        error("four-player hero restoration not published")
    end
    local deadline=scenario.Time()+4000
    while scenario.Time()<deadline do
        local observed=hero(client)
        if math.abs(observed.hit_point-10000)<.1
            and math.abs(observed.maximum_hit_point-10000)<.1 then return end
        pause(80)
    end
    error("restored hero resources did not settle")
end
local function ensureHero(client,noun)
    local observed=read(client)
    if observed.world.hero.index~=0 then
        reset(client)
        must(client.World:SelectHero(0,20000))
        observed=read(client)
    end
    if observed.world.hero.noun==noun and observed.native.is_rig_ready then return end
    local receipt=must(client.World:ReplaceHero(noun))
    if not receipt.is_replacement or not receipt.is_queued or receipt.recipient_count~=4 then
        error("native four-player hero replacement unavailable")
    end
    local deadline=scenario.Time()+8000
    while scenario.Time()<deadline do
        observed=read(client)
        if observed.world.hero.noun==noun and observed.native.is_rig_ready then return end
        pause(80)
    end
    error("replacement native rig did not settle")
end
local function place(client,position)
    local before=frame(client)
    local receipt=must(client.World:PlaceHero(position))
    if not receipt.is_queued or receipt.recipient_count~=4 then
        error("four-player hero placement not published")
    end
    local deadline=scenario.Time()+7000
    while scenario.Time()<deadline do
        local observed=read(client).native
        local dx,dy=observed.position.x-position.x,observed.position.y-position.y
        if observed.frame_sequence>before and dx*dx+dy*dy<.25 then return end
        pause(80)
    end
    error("native hero placement did not settle")
end
local function latest(client) return read(client).world.ability.last_dispatch end
local function awaitDispatch(client,previous,asset)
    local revision=previous and previous.revision or 0
    local deadline=scenario.Time()+3500
    while scenario.Time()<deadline do
        local dispatch=latest(client)
        if dispatch and dispatch.revision>revision then
            if dispatch.ability_asset~=asset then
                error("observed unexpected native ability "..tostring(dispatch.ability_asset))
            end
            return dispatch
        end
        pause(50)
    end
    error("exact Arena ability dispatch not observed: "..asset)
end
local function cast(client,x,aim,asset,isSelf)
    local previous=latest(client)
    local button=must(client.UI:Click(x,655))
    if not button.is_processed then error("native hotbar click unprocessed; no replay") end
    pause(150)
    local position={x=aim.x,y=aim.y,z=aim.z+(isSelf and 0 or 1.5)}
    click(client,position)
    return awaitDispatch(client,previous,asset)
end
local function capture(clients,label)
    for _,client in ipairs(clients) do
        local receipt=must(client.Diagnostics:Capture(label))
        record("capture",{label=label,client=client.ID,pid=client.PID,manifest=receipt.manifest_path})
    end
end
local function launch(server,id,noun)
    local profile=must(server:CreateProfile({name=id,
        hero_nouns={noun,117394854,3068979135},chain_progression=2,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=id,launch_mode="detached"}))
    must(client:Login());must(client.Scene:Wait(scene.Arsenal))
    return client
end
local function ownedTeams(server)
    local clients={}
    for index,id in ipairs(ids) do
        clients[index]=launch(server,id,index==1 and bioNoun or index==2 and missileNoun
            or index==3 and missileNoun or 749013658)
    end
    must(clients[1].Party:Create("control-team-one"))
    must(clients[1].Party:Invite(clients[2]));must(clients[2].Party:Join(clients[1]))
    must(clients[3].Party:Create("control-team-two"))
    must(clients[3].Party:Invite(clients[4]));must(clients[4].Party:Join(clients[3]))
    for _,client in ipairs(clients) do must(client.Scene:Set(scene.Navigation)) end
    for _,client in ipairs(clients) do must(client:WaitForReportedLocation("navigation",15000)) end
    must(server:StartPVP({teams={{clients[1],clients[2]},{clients[3],clients[4]}}}))
    check(scenario.Parallel({
        first=function() must(clients[1].PVP:Accept());return nil end,
        second=function() must(clients[2].PVP:Accept());return nil end,
        third=function() must(clients[3].PVP:Accept());return nil end,
        fourth=function() must(clients[4].PVP:Accept());return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(clients[1].PVP:Wait());return nil end,
        second=function() must(clients[2].PVP:Wait());return nil end,
        third=function() must(clients[3].PVP:Wait());return nil end,
        fourth=function() must(clients[4].PVP:Wait());return nil end,
    }))
    local gameID=read(clients[1]).world.game_id
    for _,client in ipairs(clients) do
        if read(client).world.game_id~=gameID then error("four clients did not enter one Arena game") end
        local window=must(client.UI:InspectWindow()).window
        if not window or window.width~=1280 or window.height~=720 then
            error("native hotbar coordinates require 1280x720 on all clients")
        end
    end
    record("pair_ready",{game=gameID,first=clients[1].PID,second=clients[2].PID,
        third=clients[3].PID,fourth=clients[4].PID})
    return clients
end
local function resetActors(clients)
    for _,client in ipairs(clients) do stop(client) end
    for _,client in ipairs(clients) do restore(client);reset(client) end
end
local function repulsionReflection(clients)
    local source,target=clients[1],clients[3]
    ensureHero(source,spaceNoun);ensureHero(target,missileNoun)
    resetActors(clients)
    local anchor=point(target)
    place(source,{x=anchor.x-20,y=anchor.y,z=anchor.z})
    local sourcePoint=point(source)
    local targetPrevious=latest(target)
    local sourcePrevious=latest(source)
    capture(clients,"repulsion-before")
    check(scenario.Parallel({
        incoming=function()
            hold(target,{x=sourcePoint.x,y=sourcePoint.y,z=sourcePoint.z+1.5})
            return nil
        end,
        reflect=function()
            awaitDispatch(target,targetPrevious,"MissileTempestBasic")
            pause(250)
            cast(source,640,point(source),"RepulsionWave",true)
            return nil
        end,
    }))
    local sourceDispatch=latest(source)
    if not sourceDispatch or sourceDispatch.revision<=(sourcePrevious and sourcePrevious.revision or 0) then
        error("Repulsion Wave did not follow the observed incoming basic")
    end
    pause(2300)
    record("repulsion_ready",{incoming_asset=latest(target).ability_asset,
        wave_asset=sourceDispatch.ability_asset,source_frame=frame(source),target_frame=frame(target)})
    capture(clients,"repulsion-after")
end
local function sleepingCloudBreak(clients)
    local source,ally,target=clients[1],clients[2],clients[3]
    ensureHero(source,bioNoun);ensureHero(ally,missileNoun);ensureHero(target,749013658)
    resetActors(clients)
    local anchor=point(target)
    place(source,{x=anchor.x-3,y=anchor.y,z=anchor.z})
    place(ally,{x=anchor.x-5,y=anchor.y+1,z=anchor.z})
    cast(source,575,anchor,"SleepingCloud",false)
    pause(350)
    capture({target},"sleep-before-hit")
    local targetBefore=hero(target)
    local previous=latest(ally)
    hold(ally,{x=anchor.x,y=anchor.y,z=anchor.z+1.5})
    awaitDispatch(ally,previous,"MissileTempestBasic")
    pause(1000)
    record("sleep_hit",{target_before=targetBefore.hit_point,target_after=hero(target).hit_point,
        ally_asset=latest(ally).ability_asset,source_frame=frame(source),target_frame=frame(target)})
    capture(clients,"sleep-after-hit")
    pause(5000) -- Let the field release before the disease fixture.
end
local function rootingPlagueSpread(clients)
    local source,ally,target,second=clients[1],clients[2],clients[3],clients[4]
    ensureHero(source,plagueNoun);ensureHero(ally,plagueNoun)
    ensureHero(target,749013658);ensureHero(second,749013658)
    resetActors(clients)
    local anchor=point(target)
    place(source,{x=anchor.x-3,y=anchor.y,z=anchor.z})
    place(ally,{x=anchor.x-4,y=anchor.y+1,z=anchor.z})
    place(second,{x=anchor.x+1,y=anchor.y+2,z=anchor.z})
    local secondBefore=hero(second)
    capture(clients,"plague-before")
    cast(source,605,anchor,"RootingPlague",false)
    pause(2500)
    local spreadHP=hero(second).hit_point
    record("plague_spread",{second_before=secondBefore.hit_point,second_after=spreadHP,
        first_after=hero(target).hit_point,source_frame=frame(source),second_frame=frame(second)})
    if spreadHP>=secondBefore.hit_point then
        capture(clients,"plague-no-spread")
        error("second opposing player has no observed Rooting Plague damage")
    end
    capture({second},"plague-after-spread")
    local previous=latest(ally)
    cast(ally,605,anchor,"RootingPlague",false)
    local next=latest(ally)
    if not next or next.revision<=(previous and previous.revision or 0) then
        error("overlapping caster was not admitted")
    end
    pause(2300)
    record("plague_overlap",{first=latest(source).ability_asset,second=next.ability_asset,
        primary_hp=hero(target).hit_point,spread_hp=hero(second).hit_point})
    capture(clients,"plague-after-overlap")
end

return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local clients=ownedTeams(server)
    repulsionReflection(clients)
    sleepingCloudBreak(clients)
    rootingPlagueSpread(clients)
    for _,client in ipairs(clients) do
        local before=frame(client)
        pause(80)
        if frame(client)<=before then error("final native frame stalled") end
    end
    record("suite_end",{is_complete=true,game_id=read(clients[1]).world.game_id})
end
