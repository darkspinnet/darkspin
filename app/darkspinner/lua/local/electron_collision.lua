---Native cursor cast against a controlled living NPC; damage is observed independently.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "electron-collision", hero_nouns = {577654149, 1667741389, 3618328019}, chain_progression = 3,
    }))
    local client = must(profile:Launch({client_id = "electron-collision", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    local worldSS = must(client.Mission:Enter(4))
    local npc = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local beforeSS = must(npc:Snapshot())
    if not beforeSS.is_available or beforeSS.is_ai_enabled or beforeSS.is_dead or beforeSS.hit_point <= 0 then
        error("Electron Sphere requires a living AI-disabled target")
    end
    must(client.Diagnostics:Capture("electron-before"))
    must(client.UI:Click(605, 655))
    check(scenario.Sleep(150))
    must(client.World:Click(beforeSS.position))
    local deadline = scenario.Time() + 12000
    while scenario.Time() < deadline do
        local afterSS = must(npc:Snapshot())
        local currentWorldSS = must(client.World:Snapshot())
        if not afterSS.is_available or currentWorldSS.game_id ~= worldSS.game_id then
            error("Electron Sphere lost its admitted world or target")
        end
        if afterSS.hit_point < beforeSS.hit_point then
            check(scenario.Log("PvE Electron Sphere target_hp=" .. beforeSS.hit_point .. " -> " .. afterSS.hit_point))
            must(client.Diagnostics:Capture("electron-after"))
            return nil
        end
        check(scenario.Sleep(200))
    end
    must(client.Diagnostics:Capture("electron-no-damage"))
    error("Electron Sphere cursor cast did not damage the intended NPC")
end
