---Two native clients: earn souls through real PvP kills, spend them, then verify death cleanup.
local must,check=scenario.Must,scenario.Check
local function pause(ms) check(scenario.Sleep(ms)) end

local function snapshot(client)
    local world=must(client.World:SnapshotLiving())
    if not world.arena or not world.arena.is_combat_active then error("owned live Arena unavailable") end
    return world
end

local function native(client)
    local state=must(client.World:SnapshotNative()).gameplay_state
    if not state or not state.is_world_active then error("native world unavailable") end
    return state
end

local function capture(clients,label)
    for _,client in ipairs(clients) do
        must(client.Diagnostics:Capture(label))
        must(client.UI:Screenshot(label))
    end
end

local function launch(server,name,mode,nouns)
    local profile=must(server:CreateProfile({name=name,hero_nouns=nouns,chain_progression=1,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=name,launch_mode=mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client,profile
end

local function place(client,point)
    local before=native(client)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("placement not queued to both owned clients") end
    local deadline=scenario.Time()+8000
    while scenario.Time()<deadline do
        local after=native(client)
        local dx,dy=after.position.x-point.x,after.position.y-point.y
        if dx*dx+dy*dy<0.09 and after.frame_sequence>before.frame_sequence then
            pause(1000)
            return
        end
        pause(100)
    end
    error("native placement not observed")
end

local function kill(source,target)
    local before=snapshot(target)
    local sourceNative=native(source)
    place(target,{x=sourceNative.position.x+3,y=sourceNative.position.y,z=sourceNative.position.z})
    for window=1,35 do
        local victim=snapshot(target)
        if victim.hero.object_id~=before.hero.object_id then
            local stop=must(source.World:Click(native(source).position))
            if not stop.is_processed then error("native stop unprocessed; no replay") end
            pause(500)
            return
        end
        local point=native(target).position
        local receipt=must(source.World:Hold(point,700))
        if not receipt.is_processed or not receipt.gameplay_click
            or not receipt.gameplay_click.is_release_poll_observed then error("native attack input incomplete; no replay") end
        pause(500)
    end
    error("bounded native attacks did not defeat the target hero")
end

local function burst(source,target,label)
    local before=snapshot(source)
    local targetBefore=snapshot(target)
    local point=native(source).position
    check(scenario.Parallel({
        cast=function()
            must(source.UI:Click(575,655))
            pause(150)
            local receipt=must(source.World:Click(point))
            if not receipt.is_processed then error("burst input unprocessed; no replay") end
            return nil
        end,
        sourceFlight=function() pause(600); must(source.UI:Screenshot(label.."-source-flight")); return nil end,
        targetFlight=function() pause(600); must(target.UI:Screenshot(label.."-opponent-flight")); return nil end,
    }))
    pause(2500)
    local after=snapshot(source)
    local targetAfter=snapshot(target)
    if after.hero.mana_point>=before.hero.mana_point then error("Phantom Burst did not spend power") end
    if after.hero.passive_kill_stack~=0 then error("Phantom Burst did not consume collected souls") end
    if targetAfter.hero.object_id==targetBefore.hero.object_id and targetAfter.hero.hit_point>=targetBefore.hero.hit_point then
        error("Phantom Burst did not damage the opposing player")
    end
    check(scenario.Log(label.." consumed="..before.hero.passive_kill_stack.." expected_shots="..
        (before.hero.passive_kill_stack==0 and 6 or 7+before.hero.passive_kill_stack)))
    capture({source,target},label)
end

local function run(clients)
    local server=must(scenario.StartServer({}))
    local source,sourceProfile=launch(server,"soul-source","normal",{43291678,185979955,171910024})
    clients[1]=source
    local target,targetProfile=launch(server,"soul-target","detached",{185979955,171910024,700180862})
    clients[2]=target
    must(source.Party:Create("soul-duel"))
    must(source.Party:Invite(target))
    must(target.Party:Join(source))
    for _,client in ipairs(clients) do must(client.Scene:Set(scene.Navigation)) end
    must(sourceProfile:WaitForReportedLocation("navigation",15000))
    must(targetProfile:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{target}}}))
    check(scenario.Parallel({
        source=function() must(source.PVP:Accept()); return nil end,
        target=function() must(target.PVP:Accept()); return nil end,
    }))
    must(source.PVP:Wait())
    must(target.PVP:Wait())
    must(source.World:SelectHero(0))
    must(target.World:SelectHero(0))
    pause(1500)
    if snapshot(source).hero.passive_kill_stack~=0 then error("fresh hero has stale souls") end
    kill(source,target)
    local earned=snapshot(source)
    if earned.hero.passive_kill_stack~=1 then error("one real PvP kill did not award exactly one soul") end
    check(scenario.Log("Real PvP kill awarded one soul"))
    capture(clients,"pvp-soul-earned")
    must(source.World:RestoreHero())
    must(target.World:RestoreHero())
    burst(source,target,"pvp-soul-spent")
    -- Resource restoration does not bypass the native ability-button cooldown.
    pause(13000)
    must(source.World:RestoreHero())
    must(target.World:RestoreHero())
    burst(source,target,"pvp-soul-empty")
    must(source.World:RestoreHero())
    must(target.World:RestoreHero())
    kill(source,target)
    if snapshot(source).hero.passive_kill_stack~=1 then error("second real kill did not award one soul") end
    must(source.World:RestoreHero())
    must(target.World:RestoreHero())
    kill(target,source)
    local defeated=snapshot(source)
    for _,member in ipairs(defeated.arena.members) do
        if member.user_id==sourceProfile.user_id and member.characters[1].passive_kill_stack~=0 then
            error("defeated hero retained its soul stack")
        end
    end
    check(scenario.Log("Defeated Arakna lost its collected soul and deployed a living reserve"))
    capture(clients,"pvp-soul-death")
end

return function()
    local clients={}
    local isComplete,failure=pcall(run,clients)
    if not isComplete then
        for _,client in ipairs(clients) do client.Diagnostics:Capture("pvp-soul-failed") end
        error(tostring(failure))
    end
end
