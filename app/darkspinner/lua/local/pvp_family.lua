---Four native clients: owned-party/match fixtures, native chains and healing channels.
---Checks enemy-only hops, ally healing, capacity expiry, cancellation and radial bursts on both teams.
local must = scenario.Must
local check = scenario.Check

local function pause(milliseconds)
    check(scenario.Sleep(milliseconds))
end

local function captureAll(participants, label)
    for _, participant in ipairs(participants) do
        must(participant.client.Diagnostics:Capture(label))
        must(participant.client.UI:Screenshot(label))
    end
end

local function launchParticipant(server, name, mode)
    local profile = must(server:CreateProfile({
        name=name, hero_nouns={171910024,185979955,117394854}, chain_progression=1, is_pvp_enabled=true,
    }))
    local client = must(profile:Launch({client_id=name,launch_mode=mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    check(scenario.Log("2v2 participant " .. name .. " user_id=" .. profile.user_id))
    return {name=name,profile=profile,client=client}
end

local function arenaSnapshot(client)
    local worldSS = must(client.World:Snapshot())
    if worldSS.arena == nil then error("owned participant lost its Arena observation") end
    return worldSS, worldSS.arena
end

local function memberSnapshot(arenaSS, userID)
    for _, memberSS in ipairs(arenaSS.members) do
        if memberSS.user_id == userID then return memberSS end
    end
    error("Arena roster missing owned user " .. userID)
end

local function acceptAndWait(participants)
    check(scenario.Parallel({
        first=function() must(participants[1].client.PVP:Accept()); return nil end,
        second=function() must(participants[2].client.PVP:Accept()); return nil end,
        third=function() must(participants[3].client.PVP:Accept()); return nil end,
        fourth=function() must(participants[4].client.PVP:Accept()); return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(participants[1].client.PVP:Wait()); return nil end,
        second=function() must(participants[2].client.PVP:Wait()); return nil end,
        third=function() must(participants[3].client.PVP:Wait()); return nil end,
        fourth=function() must(participants[4].client.PVP:Wait()); return nil end,
    }))
end

local function heroSnapshot(participant)
    local worldSS = must(participant.client.World:SnapshotLiving())
    local nativeSS = must(participant.client.World:SnapshotNative()).gameplay_state
    if worldSS.arena == nil or not worldSS.arena.is_combat_active or nativeSS == nil
        or not nativeSS.is_world_active or nativeSS.position == nil then
        error("team-healing participant lost its active native Arena hero")
    end
    return worldSS, nativeSS
end

local function separation(first, second)
    local x,y = first.x-second.x, first.y-second.y
    return math.sqrt(x*x+y*y)
end

local function place(source, point)
    local beforeSS,beforeNativeSS=heroSnapshot(source)
    local receipt=must(source.client.World:PlaceHero(point))
    if not receipt.is_queued or not receipt.is_placement or receipt.recipient_count~=4 then
        error("team fixture placement was not queued to all four owned clients")
    end
    local deadline=scenario.Time()+8000
    while scenario.Time()<deadline do
        local afterSS,afterNativeSS=heroSnapshot(source)
        if afterSS.hero.object_id~=beforeSS.hero.object_id then error("placement changed the owned hero") end
        if separation(afterNativeSS.position,point)<0.3 and afterNativeSS.frame_sequence>beforeNativeSS.frame_sequence then return end
        pause(100)
    end
    error("fixture placement did not reach the observed native point")
end

local function approach(source, target, isAttack)
    local targetSS,targetNativeSS=heroSnapshot(target)
    if isAttack then
        place(source,{x=targetNativeSS.position.x,y=targetNativeSS.position.y-2.5,z=targetNativeSS.position.z})
        return
    end
    place(source,{x=targetNativeSS.position.x-2.5,y=targetNativeSS.position.y,z=targetNativeSS.position.z})
end

local function stopAttack(participant)
    local worldSS, nativeSS = heroSnapshot(participant)
    local receipt = must(participant.client.World:Click(nativeSS.position))
    if not receipt.is_processed then error("native stop input unprocessed; no replay") end
end

local function wound(source,target,maximumFraction)
    approach(source,target,true)
    for window=1,20 do
        local beforeSS, targetNativeSS = heroSnapshot(target)
        local receipt = must(source.client.World:Hold(targetNativeSS.position,700))
        local clickSS = receipt.gameplay_click
        if not receipt.is_processed or clickSS == nil or not clickSS.is_press_poll_observed
            or not clickSS.is_release_poll_observed then
            error("ordinary wounding attack lacks paired native processing; no replay")
        end
        stopAttack(source)
        local deadline = scenario.Time()+4000
        while scenario.Time() < deadline do
            local afterSS = heroSnapshot(target)
            if afterSS.hero.object_id ~= beforeSS.hero.object_id then error("wounding attack defeated the intended hero") end
            if afterSS.hero.hit_point < beforeSS.hero.hit_point then
                check(scenario.Log("Ordinary attack wounded " .. target.name .. " HP "
                    .. beforeSS.hero.hit_point .. " -> " .. afterSS.hero.hit_point))
                if maximumFraction==nil or afterSS.hero.hit_point<=afterSS.hero.maximum_hit_point*maximumFraction then return end
                break
            end
            pause(150)
        end
        check(scenario.Log("Wounding window " .. window .. " complete; continuing until the requested health deficit is observed"))
    end
    error("bounded wounding did not produce the requested health deficit")
end

local function healthTotal(memberSS)
    local total = 0
    if #memberSS.characters ~= 3 then error("team-healing ledger lost a squad member") end
    for _, heroSS in ipairs(memberSS.characters) do
        if heroSS.hit_point > heroSS.maximum_hit_point+0.05 then error("Arena hero HP exceeds its bound maximum") end
        total=total+heroSS.hit_point
    end
    return total
end

local function maximumTotal(memberSS)
    local total = 0
    for _, heroSS in ipairs(memberSS.characters) do total=total+heroSS.maximum_hit_point end
    return total
end

local function closeEnough(first,second)
    return math.abs(first-second) <= 0.05
end

local function members(participants)
    local worldSS = must(participants[1].client.World:Snapshot())
    local snapshots = {}
    for _, participant in ipairs(participants) do
        snapshots[participant.profile.user_id] = memberSnapshot(worldSS.arena, participant.profile.user_id)
    end
    return snapshots
end

local function supportCast(participant, x, target)
    local targetSS, targetNativeSS = heroSnapshot(target)
    must(participant.client.UI:Click(x,655))
    pause(150)
    -- Native ally targeting needs the visible body, not a ground-point
    -- click that resolves to a valid explicit self target.
    local receipt = must(participant.client.World:Click({x=targetNativeSS.position.x,
        y=targetNativeSS.position.y,z=targetNativeSS.position.z+2}))
    if not receipt.is_processed then error("support input unprocessed; no replay") end
end

local function verifyChain(participants, x, label)
    local caster, ally, primary, secondary = participants[1],participants[2],participants[3],participants[4]
    for _, participant in ipairs(participants) do stopAttack(participant) end
    pause(1500)
    local baselines = members(participants)
    local sourceBeforeSS = heroSnapshot(caster)
    supportCast(caster,x,primary)
    pause(1800)
    stopAttack(caster)
    pause(300)
    local finals = members(participants)
    local sourceAfterSS = heroSnapshot(caster)
    if sourceAfterSS.hero.mana_point >= sourceBeforeSS.hero.mana_point then error(label .. " did not spend Power") end
    local damage = 0
    for _, victim in ipairs({primary,secondary}) do
        local beforeSS,afterSS = baselines[victim.profile.user_id],finals[victim.profile.user_id]
        local loss = healthTotal(beforeSS)-healthTotal(afterSS)
        if loss < 0 or not closeEnough(afterSS.damage_taken-beforeSS.damage_taken,loss) then
            error(label .. " victim health and damage ledger disagree")
        end
        damage = damage+loss
    end
    local secondaryLoss = healthTotal(baselines[secondary.profile.user_id])-healthTotal(finals[secondary.profile.user_id])
    if secondaryLoss <= 0 then error(label .. " did not independently confirm its hop to the second opponent") end
    for _, friendly in ipairs({caster,ally}) do
        if not closeEnough(healthTotal(baselines[friendly.profile.user_id]),healthTotal(finals[friendly.profile.user_id])) then
            error(label .. " damaged a friendly hero")
        end
    end
    if not closeEnough(finals[caster.profile.user_id].damage_done-baselines[caster.profile.user_id].damage_done,damage) then
        error(label .. " caster ledger differs from actual opposing HP loss")
    end
    check(scenario.Log(label .. " confirmed second opponent damage=" .. secondaryLoss .. " total=" .. damage))
    captureAll(participants,"pvp-shared-chain")
    for _,participant in ipairs(participants) do
        must(participant.client.World:RestoreHero())
    end
end

local function verifyReconstruct(participants)
    local healer,ally,enemy = participants[1],participants[2],participants[3]
    must(healer.client.World:SelectHero(1))
    wound(enemy,ally)
    for _, participant in ipairs(participants) do stopAttack(participant) end
    approach(healer,ally)
    pause(1500)
    local baselines = members(participants)
    local baseMaximum = maximumTotal(baselines[ally.profile.user_id])
    supportCast(healer,670,ally)
    pause(4300)
    local finals = members(participants)
    local allyBeforeSS,allyAfterSS = baselines[ally.profile.user_id],finals[ally.profile.user_id]
    local restored = healthTotal(allyAfterSS)-healthTotal(allyBeforeSS)
    if restored <= 0 or maximumTotal(allyAfterSS) <= baseMaximum then
        error("Reconstruct did not restore allied HP and apply its temporary capacity buff")
    end
    if not closeEnough(allyAfterSS.healing_received-allyBeforeSS.healing_received,restored)
        or not closeEnough(finals[healer.profile.user_id].healing_done-baselines[healer.profile.user_id].healing_done,restored) then
        error("Reconstruct healing ledger differs from actual allied restoration")
    end
    for _, participant in ipairs({healer,participants[3],participants[4]}) do
        local beforeSS,afterSS=baselines[participant.profile.user_id],finals[participant.profile.user_id]
        if not closeEnough(healthTotal(beforeSS),healthTotal(afterSS)) or not closeEnough(maximumTotal(beforeSS),maximumTotal(afterSS)) then
            error("Reconstruct changed an untargeted hero's health or capacity")
        end
    end
    check(scenario.Log("Reconstruct allied healing=" .. restored .. " capacity=" .. baseMaximum .. " -> " .. maximumTotal(allyAfterSS)))
    captureAll(participants,"pvp-reconstruct-complete")
    pause(13000)
    local expired = members(participants)
    local allyExpiredSS = expired[ally.profile.user_id]
    if not closeEnough(maximumTotal(allyExpiredSS),baseMaximum)
        or not closeEnough(allyExpiredSS.healing_received,allyAfterSS.healing_received) then
        error("Reconstruct capacity did not expire independently of channel release, or expiry changed healing statistics")
    end
    check(scenario.Log("Reconstruct capacity expired to " .. baseMaximum))
    wound(enemy,healer,0.5)
    for _, participant in ipairs(participants) do stopAttack(participant) end
    pause(1500)
    local selfBeforeSS = heroSnapshot(healer)
    supportCast(healer,670,healer)
    pause(900)
    local cancellingSS,cancellingNativeSS=heroSnapshot(healer)
    if cancellingSS.hero.hit_point>=cancellingSS.hero.maximum_hit_point-1 then error("cancellation requires an unfilled healing deficit") end
    local interruption=must(healer.client.World:Click({x=cancellingNativeSS.position.x-1,
        y=cancellingNativeSS.position.y+1,z=cancellingNativeSS.position.z}))
    if not interruption.is_processed then error("channel interruption input unprocessed; no replay") end
    local movementDeadline=scenario.Time()+3000
    local isMovementObserved=false
    while scenario.Time()<movementDeadline do
        local movedSS,movedNativeSS=heroSnapshot(healer)
        if separation(movedSS.hero.position,cancellingSS.hero.position)>0.15
            and separation(movedNativeSS.position,cancellingNativeSS.position)>0.15 then
            isMovementObserved=true
            break
        end
        pause(100)
    end
    if not isMovementObserved then error("channel interruption movement was not observed on server and client") end
    local stopped = members(participants)
    local stoppedWorldSS = heroSnapshot(healer)
    if stoppedWorldSS.hero.hit_point <= selfBeforeSS.hero.hit_point then error("self Reconstruct produced no heal before cancellation") end
    pause(4000)
    local settled = members(participants)
    local healerID = healer.profile.user_id
    if not closeEnough(healthTotal(stopped[healerID]),healthTotal(settled[healerID]))
        or not closeEnough(stopped[healerID].healing_done,settled[healerID].healing_done) then
        error("cancelled Reconstruct continued healing")
    end
    check(scenario.Log("Reconstruct cancellation stopped remaining ticks"))
    captureAll(participants,"pvp-reconstruct-cancelled")
end

local function verifyRadialFrom(participants,caster,ally,primary,secondary,label)
    for _,participant in ipairs(participants) do
        must(participant.client.World:RestoreHero())
    end
    must(primary.client.World:ReplaceHero(171910024))
    must(secondary.client.World:ReplaceHero(171910024))
    must(caster.client.World:ReplaceHero(43291678))
    local casterSS,casterNativeSS=heroSnapshot(caster)
    local anchor=casterNativeSS.position
    place(primary,{x=anchor.x+3,y=anchor.y,z=anchor.z})
    place(secondary,{x=anchor.x,y=anchor.y+3,z=anchor.z})
    place(ally,{x=anchor.x+3,y=anchor.y+3,z=anchor.z})
    pause(1500)
    local baselines=members(participants)
    local before=heroSnapshot(caster)
    local sourceSS,sourceNativeSS=heroSnapshot(caster)
    check(scenario.Parallel({
        cast=function()
            must(caster.client.UI:Click(575,655))
            pause(150)
            local receipt=must(caster.client.World:Click(sourceNativeSS.position))
            if not receipt.is_processed then error("radial cast input unprocessed; no replay") end
            return nil
        end,
        casterFlight=function()
            pause(500)
            must(caster.client.UI:Screenshot("pvp-radial-flight-"..label))
            return nil
        end,
        opponentFlight=function()
            pause(500)
            must(primary.client.UI:Screenshot("pvp-radial-flight-"..label))
            return nil
        end,
    }))
    pause(1800)
    local finals=members(participants)
    local after=heroSnapshot(caster)
    if after.hero.mana_point>=before.hero.mana_point then error("radial ability did not spend Power") end
    local damage=0
    for _,target in ipairs({primary,secondary}) do
        local targetID=target.profile.user_id
        local loss=healthTotal(baselines[targetID])-healthTotal(finals[targetID])
        if loss<=0 or not closeEnough(finals[targetID].damage_taken-baselines[targetID].damage_taken,loss) then
            error("radial rays did not hit both real opposing squads with matching damage statistics")
        end
        damage=damage+loss
    end
    for _,friendly in ipairs({caster,ally}) do
        local userID=friendly.profile.user_id
        if not closeEnough(healthTotal(baselines[userID]),healthTotal(finals[userID])) then error("radial rays hit a teammate") end
    end
    if not closeEnough(finals[caster.profile.user_id].damage_done-baselines[caster.profile.user_id].damage_done,damage) then
        error("radial cast ledger disagrees with actual opposing health loss")
    end
    check(scenario.Log("Radial burst "..label.." damaged both opponents for total="..damage.." without friendly damage"))
    captureAll(participants,"pvp-radial-complete-"..label)
end

local function verifyRadial(participants)
    verifyRadialFrom(participants,participants[1],participants[2],participants[3],participants[4],"team-one")
    verifyRadialFrom(participants,participants[3],participants[4],participants[1],participants[2],"team-two")
end

local function verifyAbilities(participants)
    local caster,ally,primary,secondary=participants[1],participants[2],participants[3],participants[4]
    local primarySS,primaryNativeSS=heroSnapshot(primary)
    local anchor=primaryNativeSS.position
    place(caster,{x=anchor.x-3,y=anchor.y,z=anchor.z})
    place(secondary,{x=anchor.x,y=anchor.y+3,z=anchor.z})
    place(ally,{x=anchor.x-3,y=anchor.y+3,z=anchor.z})
    local kits={
        {noun=171910024,label="Arc Weld"},
        {noun=700180862,label="Chain Lightning"},
    }
    for _,kit in ipairs(kits) do
        must(caster.client.World:ReplaceHero(kit.noun))
        pause(750)
        verifyChain(participants,640,kit.label)
    end
    verifyReconstruct(participants)
    verifyRadial(participants)
end
local function run(participants)
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    participants[1] = launchParticipant(server,"family-first","normal")
    participants[2] = launchParticipant(server,"family-second","detached")
    participants[3] = launchParticipant(server,"family-third","detached")
    participants[4] = launchParticipant(server,"family-fourth","detached")
    must(participants[1].client.Party:Create("family-one"))
    must(participants[1].client.Party:Invite(participants[2].client))
    must(participants[2].client.Party:Join(participants[1].client))
    must(participants[3].client.Party:Create("family-two"))
    must(participants[3].client.Party:Invite(participants[4].client))
    must(participants[4].client.Party:Join(participants[3].client))
    for _, participant in ipairs(participants) do
        must(participant.client.Scene:Set(scene.Navigation))
        must(participant.profile:WaitForReportedLocation("navigation",15000))
    end
    local match = must(server:StartPVP({teams={
        {participants[1].client,participants[2].client},
        {participants[3].client,participants[4].client},
    }}))
    local gameID = match.game_id
    acceptAndWait(participants)
    for _, participant in ipairs(participants) do
        local worldSS, arenaSS = arenaSnapshot(participant.client)
        if worldSS.game_id ~= gameID or #arenaSS.members ~= 4 or not arenaSS.is_combat_active then
            error("team-healing scenario did not enter one active four-player Arena")
        end
    end
    verifyAbilities(participants)
    check(scenario.Log("Shared chain, healing-channel and radial-projectile family sweep completed"))
end

return function()
    local participants = {}
    local isComplete, runError = pcall(run,participants)
    if not isComplete then
        for _, participant in ipairs(participants) do
            local capture, captureError = participant.client.Diagnostics:Capture("pvp-team-family-failed")
            check(scenario.Log("Team-healing failure client=" .. participant.name .. " manifest="
                .. tostring(capture and capture.manifest_path) .. " capture_error=" .. tostring(captureError)))
        end
        error(tostring(runError))
    end
    return nil
end
