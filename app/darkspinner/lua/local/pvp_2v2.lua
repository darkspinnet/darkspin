---Four owned clients, with explicit teams and native arena-lobby acceptance.
local must = scenario.Must
local check = scenario.Check

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

local function launchParticipant(server, name, mode)
    local profile = must(server:CreateProfile({
        name = name, hero_nouns = {1667741389, 3618328019, 749013658}, is_pvp_enabled = true,
    }))
    local client = must(profile:Launch({client_id = name, launch_mode = mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local firstClient = launchParticipant(server, "pvp-first", "normal")
    local secondClient = launchParticipant(server, "pvp-second", "detached")
    local thirdClient = launchParticipant(server, "pvp-third", "detached")
    local fourthClient = launchParticipant(server, "pvp-fourth", "detached")
    must(firstClient.Party:Create("pvp-team-first"))
    must(firstClient.Party:Invite(secondClient))
    acceptInvite(secondClient, firstClient)
    must(thirdClient.Party:Create("pvp-team-second"))
    must(thirdClient.Party:Invite(fourthClient))
    acceptInvite(fourthClient, thirdClient)
    for _, client in ipairs({firstClient, secondClient, thirdClient, fourthClient}) do
        must(client.Scene:Set(scene.Navigation))
    end
    local match = must(server:StartPVP({teams = {{firstClient, secondClient}, {thirdClient, fourthClient}}}))
    local acceptanceError = scenario.Parallel({
        first = function() must(firstClient.PVP:Accept()); return nil end,
        second = function() must(secondClient.PVP:Accept()); return nil end,
        third = function() must(thirdClient.PVP:Accept()); return nil end,
        fourth = function() must(fourthClient.PVP:Accept()); return nil end,
    })
    if acceptanceError ~= nil then
        for _, client in ipairs({firstClient, secondClient, thirdClient, fourthClient}) do
            captureArenaFailure(client)
        end
        check(acceptanceError)
    end
    for _, client in ipairs({firstClient, secondClient, thirdClient, fourthClient}) do
        local worldSS = must(client.PVP:Wait())
        if worldSS.game_id ~= match.game_id then
            return "PvP participant loaded a different arena"
        end
        must(client.Diagnostics:Capture("pvp-ready"))
    end
    return nil
end
