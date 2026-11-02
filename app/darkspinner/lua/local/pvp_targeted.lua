---Native directed/life-stealing bursts and random-target lightning rain from both teams.
local must,check=scenario.Must,scenario.Check
local function pause(ms) check(scenario.Sleep(ms)) end
local function close(first,second) return math.abs(first-second)<0.1 end
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
local function member(world,userID)
    for _,participant in ipairs(world.arena.members) do
        if participant.user_id==userID then return participant end
    end
    error("owned participant absent from Arena ledger")
end
local function capture(clients,label)
    for _,client in ipairs(clients) do
        must(client.Diagnostics:Capture(label))
        must(client.UI:Screenshot(label))
    end
end
local function place(client,point)
    local before=native(client)
    local receipt=must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("placement not queued to both owned clients") end
    local deadline=scenario.Time()+8000
    while scenario.Time()<deadline do
        local after=native(client)
        local dx,dy=after.position.x-point.x,after.position.y-point.y
        if dx*dx+dy*dy<0.09 and after.frame_sequence>before.frame_sequence then pause(1000); return end
        pause(100)
    end
    error("native placement not observed")
end
local function stop(client)
    local receipt=must(client.World:Click(native(client).position))
    if not receipt.is_processed then error("native stop not processed; no replay") end
end
local function replace(client,noun)
    local original=snapshot(client).hero.index
    must(client.World:ReplaceHero(noun))
    pause(1500)
    -- Reusing the same object ID does not refresh every native deployment
    -- reference. Use ordinary owned squad swaps, with observed readiness,
    -- before aiming at or casting from the replacement.
    must(client.World:SelectHero((original+1)%3))
    pause(5500)
    must(client.World:SelectHero(original))
    pause(1500)
end
local function wound(enemy,source)
    local point=native(source).position
    place(enemy,{x=point.x+3,y=point.y,z=point.z})
    local before=snapshot(source)
    for window=1,30 do
        local current=snapshot(source)
        if current.hero.object_id~=before.hero.object_id then error("wounding defeated the intended caster") end
        if current.hero.hit_point<=current.hero.maximum_hit_point*0.4 then stop(enemy); pause(1200); return end
        local aim=native(source).position
        local receipt=must(enemy.World:Hold({x=aim.x,y=aim.y,z=aim.z+1},700))
        if not receipt.is_processed or not receipt.gameplay_click or not receipt.gameplay_click.is_release_poll_observed then
            error("native wounding input incomplete; no replay")
        end
        stop(enemy)
        pause(600)
    end
    error("bounded wounding did not establish a healing deficit")
end
local function cast(source,target,x,label,isSelf)
    local point=native(isSelf and source or target).position
    if not isSelf then point={x=point.x,y=point.y,z=point.z+2} end
    check(scenario.Parallel({
        cast=function()
            must(source.UI:Click(x,655))
            pause(150)
            local receipt=must(source.World:Click(point))
            if not receipt.is_processed then error("native ability input unprocessed; no replay") end
            return nil
        end,
        sourceFlight=function() pause(750); must(source.UI:Screenshot(label.."-source-flight")); return nil end,
        targetFlight=function() pause(750); must(target.UI:Screenshot(label.."-opponent-flight")); return nil end,
    }))
end
local function verifyLedger(before,after,targetBefore,targetAfter)
    local oldSource,newSource=member(before,before.user_id),member(after,after.user_id)
    local oldTarget,newTarget=member(before,targetBefore.user_id),member(after,targetAfter.user_id)
    local damage=targetBefore.hero.hit_point-targetAfter.hero.hit_point
    local healing=after.hero.hit_point-before.hero.hit_point
    if damage<0 or healing<0 or not close(newSource.damage_done-oldSource.damage_done,damage)
        or not close(newTarget.damage_taken-oldTarget.damage_taken,damage)
        or not close(newSource.healing_done-oldSource.healing_done,healing)
        or not close(newSource.healing_received-oldSource.healing_received,healing)
        or not close(newTarget.healing_received,oldTarget.healing_received) then
        error("burst resource changes disagree with actual damage/healing statistics")
    end
    if after.hero.hit_point>after.hero.maximum_hit_point+0.1 then error("burst healed beyond the active Arena maximum") end
    return damage,healing
end
local function verifyRole(source,target,label)
    replace(source,43291678)
    replace(target,171910024)
    pause(1200)
    wound(target,source)
    local point=native(source).position
    place(target,{x=point.x+8,y=point.y,z=point.z})
    local before,targetBefore=snapshot(source),snapshot(target)
    cast(source,target,640,label.."-drain",false)
    pause(3500)
    local after,targetAfter=snapshot(source),snapshot(target)
    local damage,healing=verifyLedger(before,after,targetBefore,targetAfter)
    if after.hero.mana_point>=before.hero.mana_point or damage<=0 or healing<=0 or not close(healing,damage*0.5) then
        error("directed burst did not spend power, damage its enemy and restore half the actual damage")
    end
    check(scenario.Log(label.." directed burst actual damage="..damage.." healing="..healing))
    capture({source,target},label.."-drain-complete")
    pause(9000)
    must(source.World:RestoreHero())
    must(target.World:RestoreHero())
    pause(500)
    before,targetBefore=snapshot(source),snapshot(target)
    cast(source,target,640,label.."-full",false)
    pause(3500)
    after,targetAfter=snapshot(source),snapshot(target)
    damage,healing=verifyLedger(before,after,targetBefore,targetAfter)
    if after.hero.mana_point>=before.hero.mana_point or damage<=0 or healing~=0 then error("full-health burst spent no power or credited synthetic healing") end
    check(scenario.Log(label.." full-health burst damage="..damage.." healing=0"))
    capture({source,target},label.."-full-complete")
    replace(source,700180862)
    must(target.World:RestoreHero())
    pause(1200)
    before,targetBefore=snapshot(source),snapshot(target)
    cast(source,target,575,label.."-rain",true)
    pause(11500)
    after,targetAfter=snapshot(source),snapshot(target)
    damage,healing=verifyLedger(before,after,targetBefore,targetAfter)
    if after.hero.mana_point>=before.hero.mana_point or healing~=0 then error("random-target burst was not accepted or changed caster health") end
    check(scenario.Log(label.." lightning rain settled; enemy HP loss="..damage.." (zero is permitted for a defended hit)"))
    capture({source,target},label.."-rain-complete")
end
local function launch(server,name,mode)
    local profile=must(server:CreateProfile({name=name,hero_nouns={171910024,185979955,117394854},chain_progression=1,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=name,launch_mode=mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client,profile
end
local function run(clients)
    local server=must(scenario.StartServer({}))
    local first,firstProfile=launch(server,"targeted-first","normal")
    clients[1]=first
    local second,secondProfile=launch(server,"targeted-second","detached")
    clients[2]=second
    must(first.Party:Create("targeted-duel"))
    must(first.Party:Invite(second))
    must(second.Party:Join(first))
    for _,client in ipairs(clients) do must(client.Scene:Set(scene.Navigation)) end
    must(firstProfile:WaitForReportedLocation("navigation",15000))
    must(secondProfile:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{first},{second}}}))
    check(scenario.Parallel({
        first=function() must(first.PVP:Accept()); return nil end,
        second=function() must(second.PVP:Accept()); return nil end,
    }))
    must(first.PVP:Wait())
    must(second.PVP:Wait())
    verifyRole(first,second,"team-one")
    verifyRole(second,first,"team-two")
    check(scenario.Log("Directed healing bursts and retargeted rain completed from both teams in one duel"))
end
return function()
    local clients={}
    local isComplete,failure=pcall(run,clients)
    if not isComplete then
        for _,client in ipairs(clients) do client.Diagnostics:Capture("pvp-targeted-failed") end
        error(tostring(failure))
    end
end
