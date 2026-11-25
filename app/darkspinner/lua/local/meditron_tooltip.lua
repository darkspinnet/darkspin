---Observe the installed build-103 Sentry Drone tooltip without changing it.
local must, check = scenario.Must, scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name="meditron-tooltip",
        hero_nouns={0x5cdcaa89,1667741389,3618328019},chain_progression=5,
        is_overdrive_unlocked=true,
        equipped_parts={{squad_category="pve",hero_index=0,rigblock_asset_id=328,
            prefix_asset_id=149,level=20,rarity=1}}}))
    local client = must(profile:Launch({client_id="meditron-tooltip",launch_mode="normal",
        native_ui_scale_percent=100,screen_width=1280,screen_height=720,is_intro_skipped=true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    must(client.World:Ready())
    local owned = must(client:Snapshot())
    check(scenario.Log("TOOLTIP_PROCESS pid=" .. client.PID .. " instance=" .. client.InstanceID
        .. " signals=" .. owned.signal_directory))
    capture(client,"tooltip-before")
    -- Capture the undisturbed table before advancing this observer gate.
    must(client.Diagnostics:WaitForSignal("rea-armed",120000))
    for index=1,12 do
        must(client.UI:Click(1248,568))
        check(scenario.Sleep(1000))
        capture(client,"tooltip-hover-" .. index)
        -- The HUD frame is inert; avoid issuing movement orders in the world.
        must(client.UI:Click(1100,710))
        check(scenario.Sleep(500))
    end
    capture(client,"tooltip-after")
    must(client.Diagnostics:WaitForSignal("rea-complete",120000))
    return nil
end
