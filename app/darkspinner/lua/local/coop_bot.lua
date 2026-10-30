---One real localhost player searches for campaign co-op through native Matchmaker.
---The human sends no combat input; snapshots and captures retain NPC damage evidence.
local must, check = scenario.Must, scenario.Check

local function pause(milliseconds)
    check(scenario.Sleep(milliseconds))
end

local function capture(client, label)
    local captureSS = must(client.Diagnostics:Capture(label))
    must(client.UI:Screenshot(label))
    check(scenario.Log(label .. " manifest=" .. captureSS.manifest_path))
end

local function sample(client, baselineSS)
    local worldSS = must(client.World:SnapshotLiving())
    if baselineSS ~= nil and (worldSS.game_id ~= baselineSS.game_id
        or worldSS.peer_generation ~= baselineSS.peer_generation
        or worldSS.transport_generation ~= baselineSS.transport_generation
        or worldSS.zone_generation ~= baselineSS.zone_generation) then
        error("campaign companion observation changed its owned game or runtime generation")
    end
    if worldSS.arena ~= nil or worldSS.campaign == nil then
        error("companion scenario requires the committed campaign world")
    end
    return worldSS
end

local function observeCompanionCombat(client, baselineSS)
    local healthByObjectID = {}
    local deadline = scenario.Time() + 90000
    local nextCaptureAt = scenario.Time() + 15000
    while scenario.Time() < deadline do
        local worldSS = sample(client, baselineSS)
        for _, npcSS in ipairs(worldSS.npcs) do
            if npcSS.is_published and npcSS.is_hostility_available and npcSS.is_hostile
                and not npcSS.is_fixture then
                local previousHealth = healthByObjectID[npcSS.object_id]
                if previousHealth ~= nil and npcSS.hit_point < previousHealth then
                    check(scenario.Log("Campaign ambient NPC lost HP without human movement, basic attacks or abilities: object="
                        .. npcSS.object_id .. " noun=" .. npcSS.noun .. " HP=" .. previousHealth .. " -> " .. npcSS.hit_point))
                    capture(client, "coop-bot-npc-damage")
                    check(scenario.Log("Review the retained server log for Campaign AI companion spawned and packet source IDs"
                        .. "; NPC HP loss alone does not identify the damage source or certify companion pixels"))
                    return
                end
                healthByObjectID[npcSS.object_id] = npcSS.hit_point
            end
        end
        if scenario.Time() >= nextCaptureAt then
            capture(client, "coop-bot-observation")
            nextCaptureAt = scenario.Time() + 15000
        end
        pause(250)
    end
    capture(client, "coop-bot-no-damage")
    error("no ambient NPC HP loss was observed in 90 seconds without human combat input; companion combat remains unproven")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "coop-bot",
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "coop-bot", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = false}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Select(1))
    local windowSS = must(client.UI:InspectWindow()).window
    local mapSS = must(client.Mission:SnapshotSelection()).map_state
    if windowSS == nil or windowSS.width ~= 1280 or windowSS.height ~= 720
        or mapSS == nil or not mapSS.is_loaded or mapSS.state ~= 5 or mapSS.game_type ~= 2
        or mapSS.difficulty ~= 1 or mapSS.animation_state ~= 0 or mapSS.animation_difficulty ~= 1
        or mapSS.party_member_count > 1 or mapSS.is_transactions_processing
        or not mapSS.are_party_members_ready or mapSS.is_start_disabled or not mapSS.is_start_visible then
        capture(client, "coop-bot-selection-invalid")
        error("campaign Matchmaker requires the settled solo 1280x720 native 1-1 selection")
    end
    capture(client, "coop-bot-before-search")
    -- Native 1280x720 difficulty view: Matchmaker is left of Start. The retained
    -- two_mission.lua mission-selection capture shows its center at client (540,490).
    local clickSS = must(client.UI:Click(540, 490))
    if not clickSS.is_processed then error("native campaign Matchmaker input unprocessed; no replay") end
    local deadline = scenario.Time() + 45000
    local gameID = 0
    while scenario.Time() < deadline do
        local profileSS = must(profile:Snapshot())
        if profileSS.game_id ~= 0 then gameID = profileSS.game_id; break end
        pause(150)
    end
    if gameID == 0 then
        capture(client, "coop-bot-search-stalled")
        error("native campaign Matchmaker did not form a localhost companion game; no direct Start fallback")
    end
    check(scenario.Log("Solo native campaign Matchmaker formed game=" .. gameID))
    must(client.Mission:WaitDeployment())
    must(client.Mission:Continue())
    local missionSS = must(client.Mission:Wait())
    if missionSS.state.game_id ~= gameID or missionSS.state.mission.label ~= "1-1" then
        error("native campaign matchmaking entered an unexpected mission")
    end
    local baselineSS = sample(client)
    if #baselineSS.campaign.members ~= 1 or baselineSS.campaign.members[1].user_id ~= profile.user_id then
        error("companion mode must retain exactly the real human as its reward and campaign participant")
    end
    capture(client, "coop-bot-ready")
    observeCompanionCombat(client, baselineSS)
    check(scenario.Log("Owned campaign client retained for companion appearance review and manual play"))
    return nil
end
