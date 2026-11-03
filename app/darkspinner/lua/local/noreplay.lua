---Native replay campaign, ship-time /noreplay toggle and bug-report capture.
---Keep account completion independent from the authored replay override.
local must = scenario.Must
local check = scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    local image = must(client.UI:Screenshot(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path .. " image=" .. image.image_path))
end

local function chat(client, text)
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(250))
    local typed = must(client.UI:Type(text))
    if typed.processed_character_count ~= #text then error("incomplete command") end
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(750))
end

local function continueChain(client, initial)
    local deadline = scenario.Time() + 45000
    repeat
        local cinematic = must(client.Cinematic:Snapshot())
        if cinematic.is_available and not cinematic.is_active and not cinematic.is_movie_playing
            and not cinematic.is_camera_present then break end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    -- The results owner loads before its Flash cards and voting controls appear.
    -- Retain the settled frame so a missing transition is not blamed on gameplay
    -- when this finite input allowance was insufficient.
    check(scenario.Sleep(10000))
    capture(client, "noreplay-vote-ready")
    chat(client, "/noreplay on")
    chat(client, "/noreplay off")
    chat(client, "/noreplay")
    chat(client, "/noreplay invalid")
    capture(client, "noreplay-enabled-after-offer")
    -- Verified build-103 1280x720 results vote and deployment Continue controls.
    local vote = must(client.UI:Click(850, 430))
    if not vote.is_processed then error("results vote input unprocessed; no replay") end
    check(scenario.Sleep(2500))
    capture(client, "noreplay-next-deployment")
    local accept = must(client.UI:Click(915, 495))
    if not accept.is_processed then error("deployment input unprocessed; no replay") end
    deadline = scenario.Time() + 90000
    repeat
        local mission = must(client.Mission:SnapshotMission()).state
        if mission.game_id == initial.game_id and mission.mission.label == "1-2"
            and mission.session_stage == "dungeon" and mission.is_dungeon_setup_final then
            local world = must(client.Mission:Wait()).world
            if world == nil or world.zone_generation == initial.zone_generation then
                error("next mission did not establish a fresh native world generation")
            end
            if not mission.is_replay or mission.is_replay_randomized or not mission.is_noreplay then
                error("Continue did not retain authored replay override")
            end
            chat(client, "/noreplay off")
            local after = must(client.Mission:SnapshotMission()).state
            if not after.is_noreplay or after.is_replay_randomized then error("toggle changed running mission") end
            chat(client, "/bug noreplay-live-verification")
            check(scenario.Sleep(3000))
            capture(client, "noreplay-next-mission-ready")
            check(scenario.Log("Native matchmaking -> /victory -> ship -> Continue entered 1-2"))
            return
        end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    capture(client, "noreplay-next-mission-stalled")
    error("native Continue did not establish mission 1-2; no input replay")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "noreplay",
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 3}))
    local client = must(profile:Launch({client_id = "noreplay", launch_mode = "normal",
        native_ui_scale_percent = 100, screen_width = 1280, screen_height = 720,
        is_intro_skipped = true, is_cinematic_skipped = false}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Select(1))
    local window = must(client.UI:InspectWindow()).window
    local map = must(client.Mission:SnapshotSelection()).map_state
    if window == nil or window.width ~= 1280 or window.height ~= 720
        or map == nil or not map.is_loaded or map.state ~= 5 or map.game_type ~= 2
        or map.difficulty ~= 1 or map.animation_state ~= 0 or map.animation_difficulty ~= 1
        or map.party_member_count > 1 or map.is_transactions_processing
        or not map.are_party_members_ready or map.is_start_disabled or not map.is_start_visible then
        capture(client, "noreplay-selection-invalid")
        error("native Matchmaker requires settled solo 1280x720 campaign 1-1 selection")
    end
    capture(client, "noreplay-selection")
    local click = must(client.UI:Click(540, 490))
    if not click.is_processed then error("Matchmaker input unprocessed; no replay") end
    must(client.Mission:WaitDeployment())
    must(client.Mission:Continue())
    local mission = must(client.Mission:Wait())
    if mission.state.mission.label ~= "1-1" then error("unexpected initial mission") end
    if not mission.state.is_replay or not mission.state.is_replay_randomized or mission.state.is_noreplay then
        error("initial completed-stage launch was not a normal randomized replay")
    end
    local initial = must(client.World:Snapshot())
    capture(client, "noreplay-mission-ready")
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(250))
    local typed = must(client.UI:Type("/victory"))
    if typed.processed_character_count ~= #"/victory" then error("victory text incomplete; no replay") end
    must(client.UI:Screenshot("noreplay-victory-typed"))
    must(client.UI:Press("Enter"))
    local deadline = scenario.Time() + 20000
    local isComplete = false
    repeat
        local world = must(client.World:Snapshot())
        if world.game_id ~= initial.game_id or world.zone_generation ~= initial.zone_generation then
            error("mission changed before victory")
        end
        isComplete = world.boss ~= nil and world.boss.is_complete and world.boss.leader_object_id ~= 0
        if isComplete then break end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    if not isComplete then capture(client, "noreplay-victory-failed"); error("victory did not complete boss") end
    local beam = must(client.Mission:BeamOut())
    if not beam.world.is_victory_committed then error("personal victory did not commit") end
    deadline = scenario.Time() + 120000
    repeat
        local deployment = must(client.Mission:SnapshotDeployment()).deployment_state
        if deployment ~= nil and deployment.is_available and deployment.is_loaded and deployment.is_results_screen then
            capture(client, "noreplay-ship-voting")
            continueChain(client, initial)
            return nil
        end
        check(scenario.Sleep(500))
    until scenario.Time() >= deadline
    capture(client, "noreplay-ship-stalled")
    error("ship voting did not load after victory")
end
