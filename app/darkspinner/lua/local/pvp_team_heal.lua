---Four native clients: wounded ally and nearby wounded enemy beside Sage Tree of Life.
---A focused healing/ledger slice; does not force a knockout or claim persistent match completion.
local must = scenario.Must
local check = scenario.Check

local function pause(milliseconds)
    check(scenario.Sleep(milliseconds))
end

local function clickMenu(client, x, y)
    local action = must(client.UI:Click(x, y))
    if not action.is_processed then error("native menu input unprocessed; no replay") end
    pause(600)
end

local function captureAll(participants, label)
    for _, participant in ipairs(participants) do
        must(participant.client.Diagnostics:Capture(label))
        must(participant.client.UI:Screenshot(label))
    end
end

local function stop(participants, label, reason)
    captureAll(participants, label)
    error(reason .. "; owned clients retained; never replay an unknown native input")
end

local function inviteNatively(leader,targetName)
    clickMenu(leader,105,35) -- Social.
    clickMenu(leader,368,182) -- Lobby includes players in Navigation too.
    clickMenu(leader,400,210) -- Observed Search for Players edit control.
    local typed = must(leader.UI:Type(targetName))
    if typed.processed_character_count ~= #targetName then error("partial Social search input; no replay") end
    clickMenu(leader,618,210) -- Submit the native player search; typing alone does not filter Lobby.
    pause(800)
    must(leader.UI:Screenshot("pvp-2v2-invite-target"))
    clickMenu(leader,930,265) -- Native CreatePlaygroup callback is required by Navigation.
end

local function acceptInvite(client, leader)
    local windowSS = must(client.UI:InspectWindow())
    if windowSS.window == nil or windowSS.window.width ~= 1280 or windowSS.window.height ~= 720 then
        error("native party acceptance requires the verified 1280x720 layout")
    end
    pause(600)
    must(client.UI:Screenshot("pvp-party-invitation"))
    must(client.UI:Click(604, 53)) -- Observed build-103 invitation Accept.
    local deadline = scenario.Time() + 10000
    while scenario.Time() < deadline do
        local clientPartySS = must(client.Party:Snapshot())
        local leaderPartySS = must(leader.Party:Snapshot())
        if clientPartySS.party.id ~= 0 and clientPartySS.party.id == leaderPartySS.party.id
            and #clientPartySS.party.members == 2 and #leaderPartySS.party.members == 2 then
            return clientPartySS.party.id
        end
        pause(150)
    end
    must(client.Diagnostics:Capture("pvp-invite-not-accepted"))
    error("native invitation did not establish the intended two-player party; no replay")
end

local function selectTeams(client)
    local mapSS = nil
    local selectionError = nil
    local deadline = scenario.Time()+20000
    while scenario.Time() < deadline do
        local selectionSS,readError = client.Mission:SnapshotSelection()
        selectionError = readError
        mapSS = selectionSS and selectionSS.map_state
        if mapSS ~= nil and mapSS.is_loaded and mapSS.state == 1
            and mapSS.party_member_count == 2 and mapSS.is_party_leader and mapSS.are_pvp_members_ready then
            break
        end
        pause(200) -- Read-only readiness samples; no menu input has been sent.
    end
    if mapSS == nil or not mapSS.is_loaded or mapSS.state ~= 1
        or mapSS.party_member_count ~= 2 or not mapSS.is_party_leader or not mapSS.are_pvp_members_ready then
        check(scenario.Log("2v2 native Navigation guard failed: loaded=" .. tostring(mapSS and mapSS.is_loaded)
            .. " state=" .. tostring(mapSS and mapSS.state)
            .. " party_member_count=" .. tostring(mapSS and mapSS.party_member_count)
            .. " is_party_leader=" .. tostring(mapSS and mapSS.is_party_leader)
            .. " are_pvp_members_ready=" .. tostring(mapSS and mapSS.are_pvp_members_ready)
            .. " are_party_members_ready=" .. tostring(mapSS and mapSS.are_party_members_ready)
            .. " transactions=" .. tostring(mapSS and mapSS.is_transactions_processing)
            .. " game_type=" .. tostring(mapSS and mapSS.game_type)
            .. " rank=" .. tostring(mapSS and mapSS.pvp_rank_type)
            .. " size=" .. tostring(mapSS and mapSS.pvp_game_size)
            .. " read_error=" .. tostring(selectionError)))
        must(client.Diagnostics:Capture("pvp-team-navigation-invalid"))
        error("2v2 leader must observe two ready party members in Navigation")
    end
    clickMenu(client, 730, 310) -- Observed PvP control.
    pause(1000) -- Submenu slide allowance; inspect the resulting selection below.
    clickMenu(client, 530, 255) -- Observed Unranked control.
    must(client.UI:Screenshot("pvp-2v2-menu"))
    -- Installed build-103 MapRoom PlaceObject2 matrices put 2v2 exactly 198px
    -- right of 1v1 at the same Y. Settled 1v1 is observed at (535,345).
    clickMenu(client, 733, 345)
    mapSS = must(client.Mission:SnapshotSelection()).map_state
    if mapSS == nil or mapSS.state ~= 3 or mapSS.game_type ~= 3
        or mapSS.pvp_rank_type ~= 1 or mapSS.pvp_game_size ~= 1 then
        must(client.Diagnostics:Capture("pvp-2v2-selection-failed"))
        error("native PvP / Unranked / 2v2 selection not confirmed; no alternate click or fixture fallback")
    end
    check(scenario.Log("Native PvP / Unranked / 2v2 selection confirmed"))
end

local function launchParticipant(server, name, mode)
    local profile = must(server:CreateProfile({
        name=name, hero_nouns={1667741389,3618328019,749013658}, chain_progression=1, is_pvp_enabled=true,
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

local function approach(source, target)
    local deadline = scenario.Time()+45000
    while scenario.Time() < deadline do
        local sourceSS, sourceNativeSS = heroSnapshot(source)
        local targetSS, targetNativeSS = heroSnapshot(target)
        local origin, destination = sourceNativeSS.position, targetNativeSS.position
        local distance = separation(origin,destination)
        if distance <= 3 then return end
        local fraction = math.min(8,distance-2.5)/distance
        local action = must(source.client.World:Click({x=origin.x+(destination.x-origin.x)*fraction,
            y=origin.y+(destination.y-origin.y)*fraction,z=origin.z+(destination.z-origin.z)*fraction}))
        if not action.is_processed then error("team-healing approach unprocessed; no replay") end
        local isMoved = false
        for window=1,3 do
            pause(1000)
            local afterSS, afterNativeSS = heroSnapshot(source)
            if separation(origin,afterNativeSS.position) >= 0.25 then isMoved=true; break end
        end
        if not isMoved then error("team-healing approach blocked after three observation windows") end
    end
    error("team-healing approach exceeded forty-five seconds")
end

local function stopAttack(participant)
    local worldSS, nativeSS = heroSnapshot(participant)
    local receipt = must(participant.client.World:Click(nativeSS.position))
    if not receipt.is_processed then error("native stop input unprocessed; no replay") end
end

local function wound(source,target)
    approach(source,target)
    for window=1,3 do
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
                return
            end
            pause(150)
        end
        check(scenario.Log("Wounding window " .. window .. " had no confirmed damage; completed input may have been dodged"))
    end
    error("three fully observed wounding windows produced no damage")
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

local function verifyHealing(participants)
    local healer, ally, enemy = participants[1], participants[2], participants[3]
    must(healer.client.World:SelectHero(2)) -- Sage Alpha; Tree of Life is native SpecialTwo.
    wound(enemy,ally)
    wound(ally,enemy)
    for _, participant in ipairs(participants) do stopAttack(participant) end
    approach(healer,ally)
    approach(enemy,ally)
    for _, participant in ipairs(participants) do stopAttack(participant) end
    pause(1500) -- Let ordinary in-flight hits settle; the following stable sample is evidence.
    local beforeSS, healerNativeSS = heroSnapshot(healer)
    local allySS, allyNativeSS = heroSnapshot(ally)
    local enemySS, enemyNativeSS = heroSnapshot(enemy)
    if separation(healerNativeSS.position,allyNativeSS.position)>6
        or separation(healerNativeSS.position,enemyNativeSS.position)>6 then
        error("wounded ally and wounded enemy must both be near the intended Tree location")
    end
    local baselines = {}
    for _, participant in ipairs(participants) do
        baselines[participant.profile.user_id] = memberSnapshot(beforeSS.arena,participant.profile.user_id)
    end
    local healerBeforeSS, allyBeforeSS, enemyBeforeSS = baselines[healer.profile.user_id],
        baselines[ally.profile.user_id], baselines[enemy.profile.user_id]
    if healerBeforeSS.team ~= allyBeforeSS.team or healerBeforeSS.team == enemyBeforeSS.team then
        error("native 2v2 parties did not produce intended healer/ally/opponent teams")
    end
    if healthTotal(allyBeforeSS) >= maximumTotal(allyBeforeSS)
        or healthTotal(enemyBeforeSS) >= maximumTotal(enemyBeforeSS) then
        error("ordinary attacks must leave both the ally and nearby enemy wounded")
    end
    if not closeEnough(healthTotal(healerBeforeSS),maximumTotal(healerBeforeSS)) then
        error("caster must begin full to distinguish ally restoration from self-healing")
    end
    pause(1000)
    local settledSS = heroSnapshot(healer)
    for _, participant in ipairs(participants) do
        if not closeEnough(healthTotal(memberSnapshot(settledSS.arena,participant.profile.user_id)),
            healthTotal(baselines[participant.profile.user_id])) then
            error("health still changing after native stop; healing baseline is unproven")
        end
    end
    captureAll(participants,"pvp-team-heal-before")
    -- This exact build-103 Tree input sequence passed in pvp_enrage.lua.
    local action = must(healer.client.UI:Click(575,655))
    if not action.is_processed then error("Tree of Life hotbar input unprocessed; no replay") end
    pause(150)
    local targetAction = must(healer.client.World:Click(healerNativeSS.position))
    if not targetAction.is_processed then error("Tree of Life ground input unprocessed; no replay") end
    local isHealed = false
    local deadline = scenario.Time()+22000
    local finalSS = nil
    while scenario.Time() < deadline do
        finalSS = heroSnapshot(healer)
        if finalSS.hero.object_id ~= beforeSS.hero.object_id then error("Tree caster changed during observation") end
        local restored = 0
        for _, participant in ipairs(participants) do
            local baselineSS = baselines[participant.profile.user_id]
            local memberSS = memberSnapshot(finalSS.arena,participant.profile.user_id)
            local healthDelta = healthTotal(memberSS)-healthTotal(baselineSS)
            local receivedDelta = memberSS.healing_received-baselineSS.healing_received
            if memberSS.team ~= healerBeforeSS.team then
                if not closeEnough(healthDelta,0) or not closeEnough(receivedDelta,0) then
                    error("Tree changed an opponent's health/healing ledger; exclude ordinary residual damage first")
                end
            else
                if healthDelta < -0.05 or not closeEnough(receivedDelta,healthDelta) then
                    error("ally received ledger differs from actual capped squad HP restoration")
                end
                restored = restored+healthDelta
            end
            if participant ~= healer and not closeEnough(memberSS.healing_done,baselineSS.healing_done) then
                error("healing was attributed to a participant who did not cast Tree of Life")
            end
        end
        local casterSS = memberSnapshot(finalSS.arena,healer.profile.user_id)
        if not closeEnough(casterSS.healing_done-healerBeforeSS.healing_done,restored) then
            error("caster healing done differs from total actual allied HP restoration")
        end
        if restored > 0 and finalSS.hero.mana_point < beforeSS.hero.mana_point then isHealed=true end
        pause(250)
    end
    if not isHealed then error("native Tree of Life did not spend Power and restore its wounded teammate") end
    local allyAfterSS = memberSnapshot(finalSS.arena,ally.profile.user_id)
    local restored = healthTotal(allyAfterSS)-healthTotal(allyBeforeSS)
    local missing = maximumTotal(allyBeforeSS)-healthTotal(allyBeforeSS)
    if restored <= 0 or restored > missing+0.05 then error("Tree restoration exceeded missing ally HP") end
    captureAll(participants,"pvp-team-heal-after")
    check(scenario.Log("Tree of Life actual ally restoration=" .. restored .. " missing_hp=" .. missing
        .. "; caster/recipient ledgers match capped HP; nearby wounded enemy remained unchanged over22s"))
end

local function run(participants)
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    participants[1] = launchParticipant(server,"heal-first","normal")
    participants[2] = launchParticipant(server,"heal-second","detached")
    inviteNatively(participants[1].client,"heal-second")
    local firstPartyID = acceptInvite(participants[2].client,participants[1].client)
    clickMenu(participants[1].client,759,498)
    clickMenu(participants[1].client,1036,103)
    must(participants[1].client.Scene:Set(scene.Navigation))
    must(participants[2].client.Scene:Set(scene.Navigation))
    participants[3] = launchParticipant(server,"heal-third","detached")
    participants[4] = launchParticipant(server,"heal-fourth","detached")
    inviteNatively(participants[3].client,"heal-fourth")
    local secondPartyID = acceptInvite(participants[4].client,participants[3].client)
    clickMenu(participants[3].client,759,498)
    clickMenu(participants[3].client,1036,103)
    if firstPartyID==secondPartyID then error("team-healing requires independent opposing native parties") end
    must(participants[3].client.Scene:Set(scene.Navigation))
    must(participants[4].client.Scene:Set(scene.Navigation))
    selectTeams(participants[1].client)
    selectTeams(participants[3].client)
    clickMenu(participants[1].client,625,495)
    clickMenu(participants[3].client,625,495)
    local gameID = 0
    local deadline = scenario.Time()+45000
    while scenario.Time() < deadline do
        local firstSS = must(participants[1].profile:Snapshot())
        local areMatched = firstSS.game_id ~= 0
        for _, participant in ipairs(participants) do
            if must(participant.profile:Snapshot()).game_id ~= firstSS.game_id then areMatched=false end
        end
        if areMatched then gameID=firstSS.game_id; break end
        pause(150)
    end
    if gameID==0 then error("independent native 2v2 queues did not match all four healing participants") end
    acceptAndWait(participants)
    for _, participant in ipairs(participants) do
        local worldSS, arenaSS = arenaSnapshot(participant.client)
        if worldSS.game_id ~= gameID or #arenaSS.members ~= 4 or not arenaSS.is_combat_active then
            error("team-healing scenario did not enter one active four-player Arena")
        end
    end
    verifyHealing(participants)
    check(scenario.Log("Focused native 2v2 team healing slice completed; no knockout or persistent completed-match receipt asserted"))
end

return function()
    local participants = {}
    local isComplete, runError = pcall(run,participants)
    if not isComplete then
        for _, participant in ipairs(participants) do
            local capture, captureError = participant.client.Diagnostics:Capture("pvp-team-heal-failed")
            check(scenario.Log("Team-healing failure client=" .. participant.name .. " manifest="
                .. tostring(capture and capture.manifest_path) .. " capture_error=" .. tostring(captureError)))
        end
        error(tostring(runError))
    end
    return nil
end
