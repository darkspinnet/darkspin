---Native two-round Arena match followed by results Continue and ordinary ship/navigation readiness.
---Completion proves the server outcome; inspect both native result captures separately.
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
    local clickSS = action.gameplay_click
    if not action.is_processed or clickSS == nil or not clickSS.is_press_poll_observed
        or not clickSS.is_release_poll_observed then
        error("PvP basic attack lacks paired native processing; no replay")
    end
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
    check(scenario.Log("Fully processed PvP attack window produced no observed damage; dodge/admission remains distinguishable"))
    return false
end

local function exerciseKnockout(firstClient, secondClient)

    local deadline = scenario.Time()+180000
    local missedWindows = 0
    while scenario.Time() < deadline do
        local targetSS = must(secondClient.World:Snapshot())
        if targetSS.arena ~= nil and not targetSS.arena.is_combat_active and targetSS.arena.winning_team ~= 0 then
            local winnerSS = must(firstClient.World:Snapshot())
            if winnerSS.arena == nil or winnerSS.arena.is_combat_active
                or winnerSS.arena.winning_team ~= winnerSS.arena.team
                or targetSS.arena.winning_team ~= winnerSS.arena.team then
                error("PvP result winner differs between participants")
            end
            for _, member in ipairs(targetSS.arena.members) do
                if member.team == targetSS.arena.team then
                    for _, hero in ipairs(member.characters) do
                        if hero.hit_point > 0 then error("PvP ended with a living losing hero") end
                    end
                end
            end
            check(scenario.Sleep(500)) -- Observe the native overlay before the delayed branch.
            must(firstClient.Diagnostics:Capture("pvp-winner-results"))
            must(secondClient.Diagnostics:Capture("pvp-loser-results"))
            check(scenario.Log("PvP whole-team knockout and shared winner confirmed; inspect native results captures"))
            return
        end
        approach(firstClient, secondClient)
        local isProgress = damageOnce(firstClient, secondClient)
        if isProgress then
            missedWindows = 0
        else
            missedWindows = missedWindows+1
            if missedWindows >= 6 then
                must(secondClient.Diagnostics:Capture("pvp-knockout-stalled"))
                error("six fully processed attack windows made no confirmed progress; retained for packet review")
            end
        end
    end
    error("PvP team knockout did not finish within three minutes")
end




local function captureReturn(clients,label)
    for _, client in ipairs(clients) do
        local capture, captureError = client.Diagnostics:Capture(label)
        check(scenario.Log(label .. " client=" .. client.ID .. " manifest="
            .. tostring(capture and capture.manifest_path) .. " error=" .. tostring(captureError)))
        local cinematicSS, cinematicError = client.Cinematic:Snapshot()
        check(scenario.Log(label .. " presentation client=" .. client.ID
            .. " game_mode=" .. tostring(cinematicSS and cinematicSS.game_mode)
            .. " current_room=" .. tostring(cinematicSS and cinematicSS.current_room)
            .. " mode=" .. tostring(cinematicSS and cinematicSS.mode)
            .. " active=" .. tostring(cinematicSS and cinematicSS.is_active)
            .. " transitioning=" .. tostring(cinematicSS and cinematicSS.is_transitioning)
            .. " camera=" .. tostring(cinematicSS and cinematicSS.is_camera_present)
            .. " movie=" .. tostring(cinematicSS and cinematicSS.is_movie_playing)
            .. " error=" .. tostring(cinematicError)))
    end
end

local function waitValidShip(client)
    local deadline = scenario.Time()+25000
    while scenario.Time() < deadline do
        local profileSS = must(client.Scene:Snapshot())
        local cinematicSS = must(client.Cinematic:Snapshot())
        if profileSS.game_id == 0 and profileSS.is_authenticated and cinematicSS.is_available
            and cinematicSS.game_mode == 0 and (cinematicSS.current_room == 1 or cinematicSS.current_room == 2)
            and not cinematicSS.is_active and not cinematicSS.is_transitioning
            and not cinematicSS.is_camera_present and not cinematicSS.is_movie_playing then
            local room = cinematicSS.current_room == 1 and scene.Arsenal or scene.Navigation
            -- This is read-only: require the actual ship navigation controller,
            -- native room readiness and reported presence before requesting a room.
            must(client.Scene:Wait(room,15000))
            return room
        end
        check(scenario.Sleep(250))
    end
    error("results Continue did not restore a valid native ship room; no recovery scene request sent")
end

local function returnToShip(clients)
    captureReturn(clients,"pvp-return-before-continue")
    for _, client in ipairs(clients) do
        local windowSS = must(client.UI:InspectWindow()).window
        if windowSS == nil or windowSS.width ~= 1280 or windowSS.height ~= 720 then
            error("results Continue requires verified 1280x720 client bounds")
        end
        -- Observed 1282x754 whole-window results capture: Continue is centered
        -- around 1060,570; subtract the 32px titlebar for native client coordinates.
        local action = must(client.UI:Click(1060,538))
        if not action.is_processed then error("results Continue input unprocessed; no replay") end
    end
    check(scenario.Sleep(2500)) -- Presentation allowance, not successful ship entry.
    captureReturn(clients,"pvp-return-after-continue")
    local firstRoom, secondRoom = nil,nil
    check(scenario.Parallel({
        first=function() firstRoom=waitValidShip(clients[1]); return nil end,
        second=function() secondRoom=waitValidShip(clients[2]); return nil end,
    }))
    captureReturn(clients,"pvp-return-valid-ship")
    -- A normal Arsenal landing is valid. Only now perform ordinary navigation,
    -- never use Scene:Set as a way to hide a blank or unavailable ship controller.
    if firstRoom ~= scene.Navigation then must(clients[1].Scene:Set(scene.Navigation)) end
    if secondRoom ~= scene.Navigation then must(clients[2].Scene:Set(scene.Navigation)) end
    for _, client in ipairs(clients) do
        must(client.Scene:Wait(scene.Navigation))
        local mapSS = must(client.Mission:SnapshotSelection()).map_state
        if mapSS == nil or not mapSS.is_loaded or mapSS.state ~= 1 then
            error("post-match Navigation has no loaded usable main selection controller")
        end
    end
    captureReturn(clients,"pvp-return-navigation-ready")
    check(scenario.Log("Two natural rounds, results Continue, valid native ship room and ordinary Navigation readiness observed"))
end

local function run(clients)
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local firstProfile = must(server:CreateProfile({
        name = "pvp-first", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1, is_pvp_enabled = true,
    }))
    local secondProfile = must(server:CreateProfile({
        name = "pvp-second", source_login_name = firstProfile.login_name, is_source_clone_allowed = true, is_pvp_enabled = true,
    }))
    local firstClient = must(firstProfile:Launch({client_id = "pvp-first", launch_mode = "normal", is_cinematic_skipped = false}))
    local secondClient = must(secondProfile:Launch({client_id = "pvp-second", launch_mode = "detached", is_cinematic_skipped = false}))
    clients[1], clients[2] = firstClient, secondClient
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
    must(firstClient.Diagnostics:Capture("pvp-team-1-ready"))
    must(secondClient.Diagnostics:Capture("pvp-team-2-ready"))
    for roundIndex = 1, 2 do
        check(scenario.Log("Beginning observed round " .. roundIndex))
        exerciseKnockout(firstClient, secondClient)
        if roundIndex < 2 then
            local nextRoundError = scenario.Parallel({
                first = function() must(firstClient.PVP:Accept()); return nil end,
                second = function() must(secondClient.PVP:Accept()); return nil end,
            })
            check(nextRoundError)
            must(firstClient.PVP:Wait())
            must(secondClient.PVP:Wait())
        end
    end
    check(scenario.Sleep(6500))
    must(firstClient.Diagnostics:Capture("pvp-straight-set-winner"))
    must(secondClient.Diagnostics:Capture("pvp-straight-set-loser"))
    returnToShip(clients)
    return nil
end

return function()
    local clients = {}
    local isComplete, runError = pcall(run,clients)
    if not isComplete then
        captureReturn(clients,"pvp-return-failed")
        error(tostring(runError))
    end
    return nil
end
