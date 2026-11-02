---Native 1v1: Energy Sentinel Basic reduces Reconstruct's first actual heal.
---Capture manifests retain both clients' packet logs for modifier/attribute review.
local must, check = scenario.Must, scenario.Check

local function pause(ms) check(scenario.Sleep(ms)) end

local function snapshot(client)
    local world = must(client.World:SnapshotLiving())
    local native = must(client.World:SnapshotNative()).gameplay_state
    if not world.arena or not world.arena.is_combat_active or not native or not native.is_world_active
        or not native.position or not native.is_rig_ready then
        error("owned native Arena hero unavailable")
    end
    return world, native
end

local function member(world, userID)
    for _, entry in ipairs(world.arena.members) do
        if entry.user_id == userID then return entry end
    end
    error("Arena healing recipient missing from ledger")
end

local function capture(first, second, label)
    local firstCapture = must(first.Diagnostics:Capture(label.."-source"))
    local secondCapture = must(second.Diagnostics:Capture(label.."-recipient"))
    check(scenario.Log(label.." source_manifest="..firstCapture.manifest_path
        .." recipient_manifest="..secondCapture.manifest_path))
end

local function restoreWounded(client)
    local before = snapshot(client)
    local desiredHP = before.hero.maximum_hit_point * 0.4
    local receipt = must(client.World:RestoreHero({hit_point=desiredHP}))
    if not receipt.is_queued or receipt.recipient_count~=2 then error("wound fixture not published to both peers") end
    must(client.World:ResetCooldowns())
    local after = snapshot(client)
    if math.abs(after.hero.hit_point-desiredHP)>0.1 then error("wound fixture not observed") end
    return after
end

local function firstReconstructTick(client, userID, phase)
    local before, native = snapshot(client)
    local receivedBefore = member(before,userID).healing_received
    local button = must(client.UI:Click(640,655))
    if not button.is_processed then error(phase.." Reconstruct hotbar edge unprocessed") end
    pause(150)
    local cast = must(client.World:Click(native.position))
    if not cast.is_processed then error(phase.." Reconstruct target edge unprocessed") end
    local deadline = scenario.Time()+1800
    while scenario.Time()<deadline do
        local after = snapshot(client)
        if after.hero.object_id~=before.hero.object_id then error(phase.." changed recipient actor") end
        local healing = member(after,userID).healing_received-receivedBefore
        if healing>0 then
            local hpGain = after.hero.hit_point-before.hero.hit_point
            if math.abs(hpGain-healing)>0.1 then error(phase.." first heal disagrees with actual HP gain") end
            check(scenario.Log(phase.." first_reconstruct_hp_gain="..hpGain
                .." healing_received="..healing.." mana_spent="..(before.hero.mana_point-after.hero.mana_point)))
            return healing
        end
        pause(40)
    end
    error(phase.." Reconstruct produced no observed first healing tick")
end

local function launch(server, name, noun)
    local profile = must(server:CreateProfile({name=name,hero_nouns={noun,700180862,749013658},
        chain_progression=2,is_pvp_enabled=true}))
    local client = must(profile:Launch({client_id=name,launch_mode="detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client,profile
end

local function run()
    local server = must(scenario.StartServer({}))
    local source,sourceProfile = launch(server,"heal-reduction-source",171910024)
    local recipient,recipientProfile = launch(server,"heal-reduction-recipient",185979955)
    must(source.Party:Create("heal-reduction"))
    must(source.Party:Invite(recipient))
    must(recipient.Party:Join(source))
    must(source.Scene:Set(scene.Navigation))
    must(recipient.Scene:Set(scene.Navigation))
    must(sourceProfile:WaitForReportedLocation("navigation",15000))
    must(recipientProfile:WaitForReportedLocation("navigation",15000))
    must(server:StartPVP({teams={{source},{recipient}}}))
    check(scenario.Parallel({source=function() must(source.PVP:Accept()); return nil end,
        recipient=function() must(recipient.PVP:Accept()); return nil end}))
    must(source.PVP:Wait())
    must(recipient.PVP:Wait())
    local sourceWindow = must(source.UI:InspectWindow()).window
    local targetWindow = must(recipient.UI:InspectWindow()).window
    if not sourceWindow or sourceWindow.width~=1280 or sourceWindow.height~=720
        or not targetWindow or targetWindow.width~=1280 or targetWindow.height~=720 then
        error("native Reconstruct hotbar coordinates require owned 1280x720 windows")
    end
    local sourceWorld,sourceNative = snapshot(source)
    local targetWorld = snapshot(recipient)
    if sourceWorld.game_id~=targetWorld.game_id or sourceWorld.hero.noun~=171910024
        or targetWorld.hero.noun~=185979955 then error("wrong Arena or hero nouns") end
    local placed = must(recipient.World:PlaceHero({x=sourceNative.position.x+2.5,
        y=sourceNative.position.y,z=sourceNative.position.z}))
    if not placed.is_queued or placed.recipient_count~=2 then error("recipient placement not published") end
    pause(300)
    capture(source,recipient,"heal-reduction-before")

    restoreWounded(recipient)
    local baseline = firstReconstructTick(recipient,recipientProfile.user_id,"before")
    pause(3500) -- Finish the native Reconstruct channel before the next fixture.
    restoreWounded(recipient)
    local beforeHit = snapshot(recipient)
    local position = must(recipient.World:SnapshotNative()).gameplay_state.position
    local hit = must(source.World:Hold({x=position.x,y=position.y,z=position.z+1.5},600))
    local click = hit.gameplay_click
    if not hit.is_processed or not click or not click.is_press_poll_observed
        or not click.is_release_poll_observed then error("Energy Sentinel Basic lacks paired native input polls") end
    local hitDeadline = scenario.Time()+3500
    local isHit = false
    while scenario.Time()<hitDeadline do
        local afterHit = snapshot(recipient)
        if afterHit.hero.hit_point<beforeHit.hero.hit_point then isHit=true; break end
        pause(40)
    end
    if not isHit then error("Energy Sentinel Basic hit unobserved") end
    local sourcePosition = must(source.World:SnapshotNative()).gameplay_state.position
    local stop = must(source.World:Click(sourcePosition))
    if not stop.is_processed then error("Energy Sentinel attack stop unprocessed") end
    local during = firstReconstructTick(recipient,recipientProfile.user_id,"during")
    if math.abs(during-baseline*0.5)>0.15 then
        error("Energy Sentinel Basic did not halve Reconstruct's first actual HP gain")
    end
    capture(source,recipient,"heal-reduction-during")
    pause(4000) -- Channel and three-second debuff have both expired.
    restoreWounded(recipient)
    local after = firstReconstructTick(recipient,recipientProfile.user_id,"after")
    if math.abs(after-baseline)>0.15 then error("healing did not recover after debuff expiry") end
    capture(source,recipient,"heal-reduction-after")
    check(scenario.Log("Energy Sentinel Basic Reconstruct first-tick HP gains before="..baseline
        .." during="..during.." after="..after.."; inspect both manifests for EnergySentinelHealDebuff A2/A4 and attribute 30 0.5/restore"))
end

return function()
    check(scenario.RequireAPI(1))
    run()
end
