---Phase-separated production-path profiling: startup, idle AI campaign,
---three maintained gunner actors, and native victory/ship/next-mission flow.
---Combat uses a supplied high-health victim, not invulnerability or fake damage.
local must, check = scenario.Must, scenario.Check

local function finishProfile()
    local profile = must(scenario.ProfileStop())
    check(scenario.Log("Profile " .. profile.label .. " saved: " .. profile.directory))
end

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    must(client.UI:Screenshot(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path))
end

local function exerciseCombat(server)
    -- Companion actors are deliberately excluded from fixture ownership.
    -- Use a second solo world; the original AI world remains active as baseline.
    must(scenario.ProfileStart("second-client-entry"))
    local profile = must(server:CreateProfile({name = "profile-combat",
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "profile-combat", launch_mode = "normal",
        native_ui_scale_percent = 100, screen_width = 1280, screen_height = 720,
        is_intro_skipped = true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = false}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    finishProfile()
    must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    local before = must(client.World:Snapshot()).hero
    local npcs = {}
    local spawnCount = 0
    must(scenario.ProfileStart("npc-combat"))
    local deadline = scenario.Time() + 45000
    repeat
        for index = 1, 3 do
            local npc = npcs[index]
            local isReplacementNeeded = npc == nil
            if npc ~= nil then
                local snapshot = must(npc:Snapshot())
                isReplacementNeeded = not snapshot.is_available or snapshot.is_dead
            end
            if isReplacementNeeded then
                npc = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
                must(npc:SetAI(true))
                npcs[index] = npc
                spawnCount = spawnCount + 1
            end
        end
        check(scenario.Sleep(1000))
    until scenario.Time() >= deadline
    finishProfile()
    local after = must(client.World:Snapshot()).hero
    if before == nil or after == nil or after.is_defeated or after.hit_point >= before.hit_point then
        capture(client, "profile-combat-unproven")
        error("NPC workload produced no observed hero damage")
    end
    check(scenario.Log("NPC workload spawns=" .. spawnCount .. " hero_damage=" .. (before.hit_point-after.hit_point)))
    capture(client, "profile-combat-complete")
    check(client:Terminate())
end

local function continueMission(client)
    must(scenario.ProfileStart("mission-transition"))
    local initial = must(client.World:Snapshot())
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(250))
    local typed = must(client.UI:Type("/victory"))
    if typed.processed_character_count ~= #"/victory" then error("victory text incomplete; no replay") end
    must(client.UI:Press("Enter"))
    local deadline = scenario.Time() + 20000
    local isComplete = false
    repeat
        local world = must(client.World:Snapshot())
        isComplete = world.game_id == initial.game_id and world.boss ~= nil
            and world.boss.is_complete and world.boss.leader_object_id ~= 0
        if isComplete then break end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    if not isComplete then error("victory did not complete boss") end
    must(client.Mission:BeamOut())
    deadline = scenario.Time() + 120000
    local isVotingLoaded = false
    repeat
        local deployment = must(client.Mission:SnapshotDeployment()).deployment_state
        isVotingLoaded = deployment ~= nil and deployment.is_available and deployment.is_loaded and deployment.is_results_screen
        if isVotingLoaded then break end
        check(scenario.Sleep(500))
    until scenario.Time() >= deadline
    if not isVotingLoaded then error("ship voting did not load") end
    check(scenario.Sleep(10000)) -- Authored Flash voting-card animation allowance.
    capture(client, "profile-ship-vote")
    local vote = must(client.UI:Click(850, 430))
    if not vote.is_processed then error("results vote unprocessed; no replay") end
    check(scenario.Sleep(2500))
    local accept = must(client.UI:Click(915, 495))
    if not accept.is_processed then error("deployment input unprocessed; no replay") end
    deadline = scenario.Time() + 90000
    repeat
        local mission = must(client.Mission:SnapshotMission()).state
        if mission.game_id == initial.game_id and mission.mission.label == "1-2"
            and mission.session_stage == "dungeon" and mission.is_dungeon_setup_final then
            must(client.Mission:Wait())
            finishProfile()
            capture(client, "profile-next-mission")
            return
        end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    error("native Continue did not establish mission 1-2")
end

return function()
    check(scenario.RequireAPI(1))
    must(scenario.ProfileStart("startup"))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "profile-campaign",
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "profile-campaign", launch_mode = "normal",
        native_ui_scale_percent = 100, screen_width = 1280, screen_height = 720,
        is_intro_skipped = true}))
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
        error("native Matchmaker requires settled solo 1280x720 1-1 selection")
    end
    local click = must(client.UI:Click(540, 490))
    if not click.is_processed then error("Matchmaker input unprocessed; no replay") end
    must(client.Mission:WaitDeployment())
    must(client.Mission:Continue())
    must(client.Mission:Wait())
    finishProfile()
    capture(client, "profile-campaign-ready")
    must(scenario.ProfileStart("campaign-idle"))
    check(scenario.Sleep(30000))
    finishProfile()
    exerciseCombat(server)
    continueMission(client)
    return nil
end
