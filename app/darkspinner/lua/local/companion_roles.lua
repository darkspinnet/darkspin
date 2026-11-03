---Observe co-op opening role selection and idle role changes after native hero swaps.
local must, check = scenario.Must, scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    local image = must(client.UI:Screenshot(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path .. " image=" .. image.image_path))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "companion-roles",
        hero_nouns = {576185321, 749013658, 1667741389}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "companion-roles", launch_mode = "normal",
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
        capture(client, "companion-roles-selection-invalid")
        error("native Matchmaker requires settled solo 1280x720 campaign 1-1 selection")
    end
    local click = must(client.UI:Click(540, 490))
    if not click.is_processed then error("Matchmaker input unprocessed; no replay") end
    must(client.Mission:WaitDeployment())
    must(client.Mission:Continue())
    local mission = must(client.Mission:Wait())
    if mission.state.mission.label ~= "1-1" then error("unexpected companion mission") end
    check(scenario.Sleep(1500))
    capture(client, "opening-sentinel")
    must(client.World:SelectHero(1))
    check(scenario.Sleep(35000))
    capture(client, "idle-support-player")
    must(client.World:SelectHero(2))
    check(scenario.Sleep(35000))
    capture(client, "companion-roles-ready")
    check(scenario.Log("Observe the opening support choice beside Zrin, then the idle Sentinel choice after the player selects Sage; Blitz must not cause a duplicate Ravager choice"))
    return nil
end
