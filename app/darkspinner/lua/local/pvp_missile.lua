---Two owned clients using native Invite/Accept and Navigation PvP/Unranked/1v1/Start.
---Server admission, native Arena acceptance and world readiness are observed separately.
local must = scenario.Must
local check = scenario.Check

local function clickMenu(client, x, y)
    must(client.UI:Click(x, y))
    check(scenario.Sleep(600)) -- Animation allowance; subsequent snapshots establish the outcome.
end

local function inviteNatively(leader)
    local windowSS = must(leader.UI:InspectWindow())
    if windowSS.window == nil or windowSS.window.width ~= 1280 or windowSS.window.height ~= 720 then
        error("native party invitation requires the verified 1280x720 layout")
    end
    clickMenu(leader, 105, 35) -- Social.
    clickMenu(leader, 368, 182) -- Lobby; this isolated run has exactly one other player.
    clickMenu(leader, 930, 265) -- Native Invite, including native party creation/callback.
end

local function acceptInvite(client, leader)
    local windowSS = must(client.UI:InspectWindow())
    if windowSS.window == nil or windowSS.window.width ~= 1280 or windowSS.window.height ~= 720 then
        error("native party invitation acceptance requires the verified 1280x720 layout")
    end
    check(scenario.Sleep(600)) -- Delivery/animation allowance; membership below is the readiness evidence.
    must(client.UI:Screenshot("pvp-party-invitation"))
    must(client.UI:Click(604, 53)) -- Build-103 invitation Accept, as in social_flow.lua; never replay.
    local deadline = scenario.Time() + 10000
    while scenario.Time() < deadline do
        local clientPartySS = must(client.Party:Snapshot())
        local leaderPartySS = must(leader.Party:Snapshot())
        if clientPartySS.party.id ~= 0 and clientPartySS.party.id == leaderPartySS.party.id
            and #clientPartySS.party.members == 2 and #leaderPartySS.party.members == 2 then
            check(scenario.Log("Native party Accept established the shared two-player party"))
            return
        end
        check(scenario.Sleep(150))
    end
    local capture, captureError = client.Diagnostics:Capture("pvp-invite-not-accepted")
    error("native party Accept did not establish membership; no replay; manifest="
        .. tostring(capture and capture.manifest_path) .. "; capture=" .. tostring(captureError))
end

local function captureArenaFailure(client)
    local selectionSS, selectionError = client.Mission:SnapshotSelection()
    local mapSS = selectionSS and selectionSS.map_state
    check(scenario.Log("Arena acceptance failed; map_loaded=" .. tostring(mapSS and mapSS.is_loaded)
        .. "; map_state=" .. tostring(mapSS and mapSS.state) .. "; game_type=" .. tostring(mapSS and mapSS.game_type)
        .. "; party_ready=" .. tostring(mapSS and mapSS.are_party_members_ready)
        .. "; transactions=" .. tostring(mapSS and mapSS.is_transactions_processing)
        .. "; selection_error=" .. tostring(selectionError)))
    local capture, captureError = client.Diagnostics:Capture("pvp-search-stalled")
    check(scenario.Log("Arena failure manifest=" .. tostring(capture and capture.manifest_path)
        .. "; capture=" .. tostring(captureError)))
end

local function selectDuel(client)
    local beforeSS = must(client.Mission:SnapshotSelection()).map_state
    if beforeSS == nil or not beforeSS.is_loaded or beforeSS.state ~= 1
        or beforeSS.party_member_count ~= 2 or not beforeSS.are_pvp_members_ready then
        error("native Navigation must observe both party members with valid PvP squads before selection")
    end
    clickMenu(client, 730, 310) -- PvP.
    check(scenario.Sleep(1000)) -- Finish the native submenu slide before coordinate input.
    clickMenu(client, 530, 255) -- Unranked.
    clickMenu(client, 535, 345) -- 1v1.
    local menuSS = must(client.Mission:SnapshotSelection()).map_state
    if menuSS == nil or menuSS.state ~= 3 or menuSS.game_type ~= 3
        or menuSS.pvp_rank_type ~= 1 or menuSS.pvp_game_size ~= 0 then
        must(client.Diagnostics:Capture("pvp-selection-failed"))
        error("native PvP / Unranked / 1v1 selection was not confirmed; no replay")
    end
    check(scenario.Log("Native PvP / Unranked / 1v1 selection confirmed"))
end

local function nativeHero(client)
    local worldSS = must(client.World:Snapshot())
    local nativeSS = must(client.World:SnapshotNative()).gameplay_state
    if not worldSS.is_available or worldSS.hero == nil or nativeSS == nil
        or not nativeSS.is_world_active or nativeSS.position == nil then
        error("PvP combat lost its observed deployed hero")
    end
    return worldSS, nativeSS
end

local function separation(first, second)
    local x, y = first.x - second.x, first.y - second.y
    return math.sqrt(x*x + y*y)
end

local function approach(client, opponent)
    local deadline = scenario.Time() + 60000
    while scenario.Time() < deadline do
        local worldSS, nativeSS = nativeHero(client)
        local targetSS, targetNativeSS = nativeHero(opponent)
        local origin, destination = nativeSS.position, targetNativeSS.position
        local distance = separation(origin, destination)
        if distance <= 4 then return end
        local fraction = math.min(10, distance - 3) / distance
        local point = {x=origin.x+(destination.x-origin.x)*fraction,
            y=origin.y+(destination.y-origin.y)*fraction, z=origin.z+(destination.z-origin.z)*fraction}
        local action = must(client.World:Click(point))
        if not action.is_processed then error("PvP approach input unprocessed; no replay") end
        check(scenario.Sleep(1200))
        local afterSS, afterNativeSS = nativeHero(client)
        if separation(origin, afterNativeSS.position) < 0.3 then
            must(client.Diagnostics:Capture("pvp-approach-blocked"))
            local opponentSS, opponentNativeSS = nativeHero(opponent)
            local reverseOrigin = opponentNativeSS.position
            local reverseDistance = separation(reverseOrigin, origin)
            local reverseFraction = math.min(10, reverseDistance-3)/reverseDistance
            must(opponent.World:Click({x=reverseOrigin.x+(origin.x-reverseOrigin.x)*reverseFraction,
                y=reverseOrigin.y+(origin.y-reverseOrigin.y)*reverseFraction, z=reverseOrigin.z}))
            check(scenario.Sleep(1500))
            local reverseSS, reverseNativeSS = nativeHero(opponent)
            if separation(reverseOrigin, reverseNativeSS.position)<0.3 then
                error("both native approaches blocked; route requires review")
            end
        end
    end
    error("PvP approach exceeded one minute")
end

local function damageOnce(client, opponent)
    local beforeSS = must(opponent.World:Snapshot())
    local targetNativeSS = must(opponent.World:SnapshotNative()).gameplay_state
    local sourceSS = must(client.World:Snapshot())
    local action = must(client.World:Hold(targetNativeSS.position, 1000))
    if not action.is_processed then error("PvP basic attack unprocessed; no replay") end
    local deadline = scenario.Time()+6000
    while scenario.Time() < deadline do
        local targetSS = must(opponent.World:Snapshot())
        if targetSS.arena ~= nil and not targetSS.arena.is_combat_active and targetSS.arena.winning_team ~= 0 then return true end
        if targetSS.hero ~= nil and (targetSS.hero.object_id ~= beforeSS.hero.object_id
            or targetSS.hero.hit_point < beforeSS.hero.hit_point) then
            check(scenario.Log("PvP basic slot=" .. sourceSS.hero.index
                .. " target_hp=" .. beforeSS.hero.hit_point .. " -> " .. targetSS.hero.hit_point))
            return true
        end
        check(scenario.Sleep(150))
    end
    must(client.Diagnostics:Capture("pvp-damage-not-observed"))
    check(scenario.Log("PvP basic produced no observed opponent damage; recorded for packet review"))
    return nil
end


return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({content_path="D:/src/darkspin/bin/darkspinner/darkspin/cache/content.db"}))
    local firstProfile = must(server:CreateProfile({
        name = "pvp-first", hero_nouns = {3524487169, 3618328019, 117394854}, chain_progression = 2, is_pvp_enabled = true,
    }))
    local secondProfile = must(server:CreateProfile({
        name = "pvp-second", source_login_name = firstProfile.login_name, is_source_clone_allowed = true, is_pvp_enabled = true,
    }))
    local firstClient = must(firstProfile:Launch({client_id = "pvp-first", launch_mode = "normal"}))
    local secondClient = must(secondProfile:Launch({client_id = "pvp-second", launch_mode = "detached"}))
    must(firstClient:Login())
    must(secondClient:Login())
    must(firstClient.Scene:Wait(scene.Arsenal))
    must(secondClient.Scene:Wait(scene.Arsenal))

    inviteNatively(firstClient)
    acceptInvite(secondClient, firstClient)
    local firstPartySS = must(firstClient.Party:Snapshot())
    local secondPartySS = must(secondClient.Party:Snapshot())
    if firstPartySS.party.id == 0 or firstPartySS.party.id ~= secondPartySS.party.id
        or #firstPartySS.party.members ~= 2 or #secondPartySS.party.members ~= 2 then
        return "1v1 requires both owned clients in the same two-player party"
    end
    clickMenu(firstClient, 759, 498) -- Dismiss the native invitation confirmation.
    clickMenu(firstClient, 1036, 103) -- Close Social.
    must(firstClient.Scene:Set(scene.Navigation))
    must(secondClient.Scene:Set(scene.Navigation))
    selectDuel(firstClient)

    clickMenu(firstClient, 625, 495) -- Native Start; preserve its real request/callback sequence.
    local match = {game_id = 0}
    local admissionDeadline = scenario.Time() + 15000
    while scenario.Time() < admissionDeadline do
        local firstProfileSS = must(firstProfile:Snapshot())
        local secondProfileSS = must(secondProfile:Snapshot())
        if firstProfileSS.game_id ~= 0 and firstProfileSS.game_id == secondProfileSS.game_id then
            match.game_id = firstProfileSS.game_id
            break
        end
        check(scenario.Sleep(150))
    end
    if match.game_id == 0 then
        captureArenaFailure(firstClient)
        captureArenaFailure(secondClient)
        error("native PvP Start did not admit both party members; no replay")
    end
    local acceptanceError = scenario.Parallel({
        first = function()
            must(firstClient.PVP:Accept())
            return nil
        end,
        second = function()
            must(secondClient.PVP:Accept())
            return nil
        end,
    })
    if acceptanceError ~= nil then
        captureArenaFailure(firstClient)
        captureArenaFailure(secondClient)
        check(acceptanceError)
    end
    local firstWorldSS = must(firstClient.PVP:Wait())
    local secondWorldSS = must(secondClient.PVP:Wait())
    if firstWorldSS.game_id ~= match.game_id or secondWorldSS.game_id ~= match.game_id then
        return "PvP participants did not load the formed arena together"
    end
    approach(firstClient, secondClient)
    if not damageOnce(firstClient,secondClient) then error("SRS-42 travelling basic produced no opponent damage") end
    check(scenario.Sleep(2000))
    local targetNativeSS = must(secondClient.World:SnapshotNative()).gameplay_state
    local castResult = nil
    check(scenario.Parallel({
        cast = function()
            castResult = must(firstClient.Ability:Cast(targetNativeSS.position, 60000))
            return nil
        end,
        observe = function()
            for index=1,20 do
                local firstSS = must(firstClient.World:Snapshot())
                local secondSS = must(secondClient.World:Snapshot())
                check(scenario.Log("Rocket Barrage sample=" .. index .. " source_hp=" .. tostring(firstSS.hero and firstSS.hero.hit_point)
                    .. " target_hp=" .. tostring(secondSS.hero and secondSS.hero.hit_point)))
                check(scenario.Sleep(250))
            end
            return nil
        end,
    }))
    local castSS = castResult and castResult.observation and castResult.observation.state.cast
    if not castResult or not castResult.is_completed or not castSS or not castSS.is_accepted
        or not castSS.is_released or not castSS.is_burst_completed or castSS.is_failed
        or castSS.expected_launch_count <= 0 or castSS.launch_count ~= castSS.expected_launch_count then
        error("PvP Rocket Barrage did not complete every authored launch, release and retirement")
    end
    check(scenario.Log("PvP Rocket Barrage completed launches=" .. castSS.launch_count))
    must(firstClient.Diagnostics:Capture("pvp-shared-projectile-source"))
    must(secondClient.Diagnostics:Capture("pvp-shared-projectile-target"))
    return nil
end
