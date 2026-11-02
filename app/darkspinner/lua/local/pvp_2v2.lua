---Four real clients: native two-party 2v2 queues, two complete team knockouts and results.
---Durable receipts are checked externally in this isolated server's database.
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

local function livingCount(memberSS)
    if #memberSS.characters ~= 3 then error("Arena member does not retain its three owned heroes") end
    local count = 0
    for _, heroSS in ipairs(memberSS.characters) do
        if heroSS.hit_point > 0 then count = count + 1 end
    end
    return count
end

local function verifyRoster(participants, expectedGame)
    local teams = {}
    for _, participant in ipairs(participants) do
        local worldSS, arenaSS = arenaSnapshot(participant.client)
        if worldSS.game_id ~= expectedGame or #arenaSS.members ~= 4 or not arenaSS.is_combat_active then
            stop(participants,"pvp-four-player-roster-invalid","native ready did not establish four active members in one Arena")
        end
        local seen, counts = {}, {[1]=0,[2]=0}
        for _, memberSS in ipairs(arenaSS.members) do
            if seen[memberSS.user_id] or counts[memberSS.team] == nil then error("invalid or duplicate Arena roster") end
            seen[memberSS.user_id] = true
            counts[memberSS.team] = counts[memberSS.team] + 1
            if livingCount(memberSS) ~= 3 then error("2v2 round did not begin with full living squads") end
        end
        if counts[1] ~= 2 or counts[2] ~= 2 then error("2v2 must admit two players per team") end
        for _, owned in ipairs(participants) do
            local memberSS = memberSnapshot(arenaSS,owned.profile.user_id)
            if teams[owned.profile.user_id] ~= nil and teams[owned.profile.user_id] ~= memberSS.team then
                error("participants disagree on team membership")
            end
            teams[owned.profile.user_id] = memberSS.team
        end
    end
    if teams[participants[1].profile.user_id] ~= teams[participants[2].profile.user_id]
        or teams[participants[3].profile.user_id] ~= teams[participants[4].profile.user_id]
        or teams[participants[1].profile.user_id] == teams[participants[3].profile.user_id] then
        error("native matchmaker split an established party or combined opposing parties")
    end
    return teams[participants[1].profile.user_id]
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

local function nativeHero(client)
    local worldSS = must(client.World:Snapshot())
    local nativeSS = must(client.World:SnapshotNative()).gameplay_state
    if not worldSS.is_available or worldSS.hero == nil or worldSS.hero.hit_point <= 0
        or nativeSS == nil or not nativeSS.is_world_active or nativeSS.position == nil then
        return nil,nil
    end
    return worldSS,nativeSS
end

local function separation(first,second)
    local x,y = first.x-second.x,first.y-second.y
    return math.sqrt(x*x+y*y)
end

local function approach(participants, source, target)
    local deadline = scenario.Time()+45000
    local blocked = 0
    while scenario.Time() < deadline do
        local sourceSS,sourceNativeSS = nativeHero(source.client)
        local targetSS,targetNativeSS = nativeHero(target.client)
        if sourceSS ~= nil and targetSS ~= nil then
            local origin,destination = sourceNativeSS.position,targetNativeSS.position
            local distance = separation(origin,destination)
            if distance <= 3.5 then return end
            local fraction = math.min(10,distance-3)/distance
            local action = must(source.client.World:Click({x=origin.x+(destination.x-origin.x)*fraction,
                y=origin.y+(destination.y-origin.y)*fraction,z=origin.z+(destination.z-origin.z)*fraction}))
            if not action.is_processed then error("2v2 approach input unprocessed; no replay") end
            pause(1000)
            local afterSS,afterNativeSS = nativeHero(source.client)
            if afterSS ~= nil and separation(origin,afterNativeSS.position)<0.3 then
                blocked = blocked+1
                if blocked >= 3 then
                    stop(participants,"pvp-2v2-route-blocked","three confirmed native approach windows made no movement")
                end
            else
                blocked = 0
            end
        else
            pause(150) -- Automatic reserve deployment must be observed before using its pose.
        end
    end
    stop(participants,"pvp-2v2-approach-timeout","2v2 approach exceeded its bounded window")
end

local function damageWindow(source,target)
    local beforeSS,targetNativeSS = nativeHero(target.client)
    if beforeSS == nil then pause(150); return false end
    local action = must(source.client.World:Hold(targetNativeSS.position,1000))
    if not action.is_processed then error("2v2 attack input unprocessed; no replay") end
    local deadline = scenario.Time()+2500
    while scenario.Time() < deadline do
        local targetSS,arenaSS = arenaSnapshot(target.client)
        if livingCount(memberSnapshot(arenaSS,target.profile.user_id)) == 0 then return true end
        if targetSS.hero ~= nil and (targetSS.hero.object_id ~= beforeSS.hero.object_id
            or targetSS.hero.hit_point < beforeSS.hero.hit_point) then
            check(scenario.Log("2v2 observed damage " .. source.name .. " -> " .. target.name
                .. " HP " .. beforeSS.hero.hit_point .. " -> " .. targetSS.hero.hit_point))
            return true
        end
        pause(150)
    end
    check(scenario.Log("2v2 no confirmed damage in this processed-input window; dodge/arrival/admission remains distinguishable"))
    return false
end

local function defeatMember(participants,source,target,otherTarget,winningTeam)
    local deadline = scenario.Time()+240000
    local missedWindows = 0
    while scenario.Time() < deadline do
        local worldSS,arenaSS = arenaSnapshot(source.client)
        local targetMemberSS = memberSnapshot(arenaSS,target.profile.user_id)
        if livingCount(targetMemberSS) == 0 then
            if otherTarget ~= nil and livingCount(memberSnapshot(arenaSS,otherTarget.profile.user_id)) > 0 then
                if not arenaSS.is_combat_active or arenaSS.winning_team ~= 0 then
                    stop(participants,"pvp-2v2-premature-win","round ended after one opponent while its teammate still had living reserves")
                end
                check(scenario.Log("First opponent's three heroes defeated; teammate alive and Arena combat correctly continues"))
            end
            return
        end
        if not arenaSS.is_combat_active or arenaSS.winning_team ~= 0 then
            stop(participants,"pvp-2v2-premature-round-end","Arena ended before the targeted member lost its whole squad")
        end
        approach(participants,source,target)
        if damageWindow(source,target) then
            missedWindows = 0
        else
            missedWindows = missedWindows+1
            if missedWindows >= 6 then
                stop(participants,"pvp-2v2-combat-no-progress","six fully observed attack windows produced no confirmed damage")
            end
        end
    end
    stop(participants,"pvp-2v2-knockout-timeout","2v2 member knockout exceeded four minutes")
end

local function verifyRound(participants,winningTeam,isFinal)
    local deadline = scenario.Time()+10000
    while scenario.Time() < deadline do
        local areComplete = true
        local completedArenaSS = nil
        for _, participant in ipairs(participants) do
            local worldSS,arenaSS = arenaSnapshot(participant.client)
            if completedArenaSS == nil then completedArenaSS = arenaSS end
            if arenaSS.is_combat_active or arenaSS.winning_team == 0 then
                areComplete = false
            elseif arenaSS.winning_team ~= winningTeam then
                stop(participants,"pvp-2v2-winner-disagreement","participant observed the wrong winning team")
            else
                if #arenaSS.members ~= 4 then error("completed Arena result lost a roster member") end
                for _, memberSS in ipairs(arenaSS.members) do
                    if memberSS.team ~= winningTeam and livingCount(memberSS) ~= 0 then
                        error("2v2 outcome retained a living losing hero")
                    end
                end
                if not isFinal and arenaSS.is_result_ready then error("one round incorrectly completed a best-of-three match") end
            end
        end
        if areComplete then return completedArenaSS end
        pause(150)
    end
    stop(participants,"pvp-2v2-outcome-timeout","four participants did not observe one shared whole-team result")
end

local function probeSwapAndSphere(participants)
    local client = participants[2].client
    pause(5200) -- Respect ordinary swap cooldown; SelectHero then proves native/server completion.
    must(client.World:SelectHero(1))
    local beforeSS = must(client.World:Snapshot())
    local action = must(client.UI:Click(605,684)) -- Observed 1280x720 Random slot; Vex Delaying Sphere.
    if not action.is_processed then error("2v2 Random input unprocessed; no replay") end
    pause(1800)
    local afterSS = must(client.World:Snapshot())
    check(scenario.Log("2v2 Vex Delaying Sphere probe: mana " .. beforeSS.hero.mana_point
        .. " -> " .. tostring(afterSS.hero and afterSS.hero.mana_point)
        .. "; native acceptance/field/recipient evidence requires packet review"))
    must(client.Diagnostics:Capture("pvp-2v2-delaying-sphere"))
    pause(3500)
    must(client.World:SelectHero(0))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local participants = {
        launchParticipant(server,"pvp-first","normal"),
        launchParticipant(server,"pvp-second","detached"),
    }
    inviteNatively(participants[1].client,"pvp-second")
    local firstPartyID = acceptInvite(participants[2].client,participants[1].client)
    clickMenu(participants[1].client,759,498)
    clickMenu(participants[1].client,1036,103)
    must(participants[1].client.Scene:Set(scene.Navigation))
    must(participants[2].client.Scene:Set(scene.Navigation))
    participants[3] = launchParticipant(server,"pvp-third","detached")
    participants[4] = launchParticipant(server,"pvp-fourth","detached")
    inviteNatively(participants[3].client,"pvp-fourth")
    local secondPartyID = acceptInvite(participants[4].client,participants[3].client)
    clickMenu(participants[3].client,759,498)
    clickMenu(participants[3].client,1036,103)
    if firstPartyID == secondPartyID then error("2v2 requires two independent accepted parties") end
    must(participants[3].client.Scene:Set(scene.Navigation))
    must(participants[4].client.Scene:Set(scene.Navigation))
    selectTeams(participants[1].client)
    selectTeams(participants[3].client)
    clickMenu(participants[1].client,625,495) -- First party leader's native Start.
    clickMenu(participants[3].client,625,495) -- Independent opposing party's native Start.
    local gameID = 0
    local deadline = scenario.Time()+45000
    while scenario.Time() < deadline do
        local firstSS = must(participants[1].profile:Snapshot())
        local areMatched = firstSS.game_id ~= 0
        for _, participant in ipairs(participants) do
            local profileSS = must(participant.profile:Snapshot())
            if profileSS.game_id ~= firstSS.game_id then areMatched = false end
        end
        if areMatched then gameID=firstSS.game_id; break end
        pause(150)
    end
    if gameID == 0 then stop(participants,"pvp-2v2-search-stalled","native two-party 2v2 queues did not admit all four accounts") end
    check(scenario.Log("Native 2v2 matchmaker admitted all four owned profiles to game " .. gameID))
    local finalArenaSS = nil
    for roundIndex=1,2 do
        acceptAndWait(participants)
        local winningTeam = verifyRoster(participants,gameID)
        captureAll(participants,"pvp-2v2-round-" .. roundIndex .. "-ready")
        if roundIndex == 1 then probeSwapAndSphere(participants) end
        defeatMember(participants,participants[1],participants[3],participants[4],winningTeam)
        defeatMember(participants,participants[2],participants[4],nil,winningTeam)
        local completedArenaSS = verifyRound(participants,winningTeam,roundIndex==2)
        if roundIndex == 2 then finalArenaSS = completedArenaSS end
        must(participants[1].client.UI:Screenshot("pvp-2v2-round-" .. roundIndex .. "-overlay"))
        check(scenario.Log("2v2 round " .. roundIndex .. " ended only after both losing squads were defeated"))
    end
    pause(6500) -- Final branch allowance; native result receipt remains a separate captured fact.
    captureAll(participants,"pvp-2v2-final-results")
    for _, participant in ipairs(participants) do
        -- Results entry can retire live gameplay membership. Retain the final
        -- complete knockout ledger instead of querying an already departed peer.
        local memberSS = memberSnapshot(finalArenaSS,participant.profile.user_id)
        check(scenario.Log("2v2 final user_id=" .. participant.profile.user_id .. " team=" .. memberSS.team
            .. " damage_done=" .. memberSS.damage_done .. " damage_taken=" .. memberSS.damage_taken
            .. " healing_done=" .. memberSS.healing_done .. " healing_received=" .. memberSS.healing_received))
    end
    check(scenario.Log("2v2 two real rounds complete; inspect all four native result captures and persisted match receipts"))
    return nil
end
