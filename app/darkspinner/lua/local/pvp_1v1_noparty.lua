---Two independent players using native Navigation PvP/Unranked/1v1 matchmaking without a shared party.
---Server admission, native Arena acceptance and world readiness are observed separately.
local must = scenario.Must
local check = scenario.Check

local function clickMenu(client, x, y)
    must(client.UI:Click(x, y))
    check(scenario.Sleep(600)) -- Animation allowance; subsequent snapshots establish the outcome.
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
        or beforeSS.party_member_count > 1 or not beforeSS.are_pvp_members_ready then
        error("native Navigation must observe a solo player with a valid PvP squad before selection")
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

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local firstProfile = must(server:CreateProfile({
        name = "pvp-first", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1, is_pvp_enabled = true,
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

    local firstPartySS = must(firstClient.Party:Snapshot())
    local secondPartySS = must(secondClient.Party:Snapshot())
    if firstPartySS.party.id ~= 0 and firstPartySS.party.id == secondPartySS.party.id then
        error("no-party matchmaking requires independent players")
    end
    check(scenario.Log("Independent players confirmed; no invitation or shared party created"))
    must(firstClient.Scene:Set(scene.Navigation))
    must(secondClient.Scene:Set(scene.Navigation))
    selectDuel(firstClient)
    selectDuel(secondClient)

    clickMenu(firstClient, 625, 495) -- Queue the first solo player through native Start.
    clickMenu(secondClient, 625, 495) -- Queue the second independently; no server admission shortcut.
    local match = {game_id = 0}
    local admissionDeadline = scenario.Time() + 45000
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
        error("independent native 1v1 queues did not match both solo players; no replay")
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
    return nil
end
