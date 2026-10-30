---One real localhost client fills native 1v1 and 2v2 queues with server bots.
---Each mode owns exactly one authenticated human and uses native menu/input.
local must, check = scenario.Must, scenario.Check

local function pause(milliseconds)
    check(scenario.Sleep(milliseconds))
end

local function capture(client, label)
    must(client.Diagnostics:Capture(label))
    must(client.UI:Screenshot(label))
end

local function clickMenu(client, x, y)
    capture(client, string.format("menu-%d-%d-before", x, y))
    local receipt = must(client.UI:Click(x, y))
    if not receipt.is_processed then error("native Arena menu input unprocessed; no replay") end
    pause(600)
end

local function ownMember(arenaSS, userID)
    for _, memberSS in ipairs(arenaSS.members) do
        if memberSS.user_id == userID then return memberSS end
    end
    error("Arena snapshot omitted the real owned player")
end

local function roster(client, profile, expectedCount)
    local worldSS = must(client.World:Snapshot())
    local arenaSS = worldSS.arena
    if arenaSS == nil or not arenaSS.is_combat_active or #arenaSS.members ~= expectedCount then
        capture(client, "bot-roster-invalid")
        error("Arena did not establish the expected bot-filled combat roster")
    end
    local counts, seen = {[1] = 0, [2] = 0}, {}
    for _, memberSS in ipairs(arenaSS.members) do
        if seen[memberSS.user_id] or counts[memberSS.team] == nil then
            error("Arena bot roster contains an invalid identity or team")
        end
        seen[memberSS.user_id] = true
        counts[memberSS.team] = counts[memberSS.team] + 1
        if #memberSS.characters ~= 3 then error("Arena bot lacks its complete hero squad") end
    end
    if counts[1] ~= expectedCount / 2 or counts[2] ~= expectedCount / 2 then
        error("Arena bot fallback did not balance the two teams")
    end
    ownMember(arenaSS, profile.user_id)
    return worldSS
end

local function fight(client, profile, baselineSS, label)
    local baselineMemberSS = ownMember(baselineSS.arena, profile.user_id)
    local deadline = scenario.Time() + 45000
    local nextAttackAt = 0
    local isNativeAttackSent = false
    while scenario.Time() < deadline do
        local worldSS = must(client.World:Snapshot())
        local arenaSS = worldSS.arena
        if arenaSS == nil then error("owned player lost its Arena combat observation") end
        local memberSS = ownMember(arenaSS, profile.user_id)
        local isBotDamageObserved = memberSS.damage_taken > baselineMemberSS.damage_taken
        local isHumanDamageObserved = memberSS.damage_done > baselineMemberSS.damage_done
        if isBotDamageObserved and isHumanDamageObserved then
            check(scenario.Log(label .. " combat confirmed: human damage=" .. memberSS.damage_done
                .. "; received=" .. memberSS.damage_taken .. "; native_attack=" .. tostring(isNativeAttackSent)))
            capture(client, label .. "-combat")
            return
        end
        if not arenaSS.is_combat_active then
            capture(client, label .. "-round-ended")
            error("Arena round ended before bidirectional human/bot damage was observed")
        end
        if scenario.Time() >= nextAttackAt then
            local targetSS = nil
            for _, candidateSS in ipairs(arenaSS.members) do
                if candidateSS.team ~= arenaSS.team then
                    for _, heroSS in ipairs(candidateSS.characters) do
                        if heroSS.index == candidateSS.deployed_index and heroSS.hit_point > 0
                            and heroSS.object_id ~= 0 and heroSS.position ~= nil then
                            targetSS = heroSS
                            break
                        end
                    end
                    if targetSS ~= nil then break end
                end
            end
            if targetSS ~= nil then
                local receipt = must(client.World:Hold(targetSS.position, 1000))
                if not receipt.is_processed then error("native attack input unprocessed; no replay") end
                isNativeAttackSent = true
            end
            nextAttackAt = scenario.Time() + 1400
        end
        pause(150)
    end
    capture(client, label .. "-combat-stalled")
    error("45-second native combat window did not confirm human and bot damage")
end

local function waitArenaNavigation(client, label)
    local mapSS = nil
    local selectionError = nil
    local deadline = scenario.Time() + 20000
    while scenario.Time() < deadline do
        local selectionSS
        selectionSS, selectionError = client.Mission:SnapshotSelection()
        mapSS = selectionSS and selectionSS.map_state
        if mapSS ~= nil and mapSS.is_loaded and mapSS.state == 1
            and mapSS.party_member_count <= 1 and mapSS.are_pvp_members_ready then
            return mapSS
        end
        pause(200)
    end
    check(scenario.Log(label .. " navigation wait failed: loaded=" .. tostring(mapSS and mapSS.is_loaded)
        .. " state=" .. tostring(mapSS and mapSS.state)
        .. " party_member_count=" .. tostring(mapSS and mapSS.party_member_count)
        .. " are_pvp_members_ready=" .. tostring(mapSS and mapSS.are_pvp_members_ready)
        .. " read_error=" .. tostring(selectionError)))
    capture(client, label .. "-navigation-invalid")
    error("bot fallback requires a settled solo player with a valid native PvP squad")
end

local function runMode(server, expectedCount)
    local label = expectedCount == 2 and "bots-1v1" or "bots-2v2"
    local profile = must(server:CreateProfile({name = label,
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1, is_pvp_enabled = true}))
    local client = must(profile:Launch({client_id = label, launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.Scene:Set(scene.Navigation))
    local mapSS = waitArenaNavigation(client, label)
    clickMenu(client, 730, 310)
    pause(1000)
    clickMenu(client, 530, 255)
    clickMenu(client, expectedCount == 2 and 535 or 733, 345)
    mapSS = must(client.Mission:SnapshotSelection()).map_state
    local expectedSize = expectedCount == 2 and 0 or 1
    if mapSS == nil or mapSS.state ~= 3 or mapSS.game_type ~= 3
        or mapSS.pvp_rank_type ~= 1 or mapSS.pvp_game_size ~= expectedSize then
        capture(client, label .. "-selection-invalid")
        error("native Unranked Arena size selection was not confirmed; no replay")
    end
    clickMenu(client, 625, 495)
    local deadline = scenario.Time() + 45000
    local gameID = 0
    while scenario.Time() < deadline do
        local profileSS = must(profile:Snapshot())
        if profileSS.game_id ~= 0 then gameID = profileSS.game_id; break end
        pause(150)
    end
    if gameID == 0 then
        capture(client, label .. "-queue-stalled")
        error("solo native queue did not form a bot-filled Arena")
    end
    check(scenario.Log(label .. " native solo queue formed game=" .. gameID))
    must(client.PVP:Accept())
    must(client.PVP:Wait())
    local baselineSS = roster(client, profile, expectedCount)
    capture(client, label .. "-ready")
    fight(client, profile, baselineSS, label)
    if expectedCount == 2 then
        check(client:Close())
        check(profile:Logout())
    else
        check(scenario.Log("Final owned 2v2 Arena retained for manual play and inspection"))
    end
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    runMode(server, 2)
    runMode(server, 4)
    check(scenario.Log("Single-human native 1v1 and 2v2 bot fallback completed"))
    return nil
end
