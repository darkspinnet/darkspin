---Live native-chat check for /ss dump, /kill, /noreplay and /victory.
local must, check = scenario.Must, scenario.Check

local function waitFor(label, observe, timeout)
    local deadline = scenario.Time() + (timeout or 20000)
    repeat
        local result = observe()
        if result then check(scenario.Log("PASS " .. label)); return result end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error(label .. " timed out; no command resubmission")
end

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    check(scenario.Log("EVIDENCE " .. label .. " " .. report.manifest_path))
end

local function send(client, text, expected)
    local mark = must(client.Chat:Snapshot())
    if not must(client.UI:Press("Enter")).is_processed then error("chat opening was not processed") end
    check(scenario.Sleep(1000))
    local typed = must(client.UI:Type(text))
    if typed.processed_character_count ~= #text then error("partial command input; no retry") end
    if not must(client.UI:Press("Enter")).is_processed then error("chat submission was not processed") end
    local reply = must(client.Chat:Wait(mark.cursor, string.match(text, "^(/[%a]+)"), 30000))
    if reply.source ~= "server_command_reply" or reply.is_native_rendered ~= false or reply.body ~= text
        or not string.find(reply.reply, expected, 1, true) then error("unexpected reply for " .. text .. ": " .. reply.reply) end
    -- The server reply can arrive before the native chat field finishes closing.
    check(scenario.Sleep(1500))
    check(scenario.Log("REPLY " .. text .. " => " .. reply.reply))
    return reply
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name="commands-focus", is_admin=true,
        hero_nouns={1667741389,3618328019,749013658}, chain_progression=1}))
    local client = must(profile:Launch({client_id="commands-focus", launch_mode="normal",
        native_ui_scale_percent=100, is_intro_skipped=true, is_cinematic_skipped=true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    send(client, "/noreplay on", "No replay ON")
    if not must(profile:Snapshot()).is_noreplay_enabled then error("noreplay on did not persist to login") end
    send(client, "/noreplay off", "No replay OFF")
    if must(profile:Snapshot()).is_noreplay_enabled then error("noreplay off did not clear login toggle") end
    send(client, "/noreplay", "No replay ON")
    if not must(profile:Snapshot()).is_noreplay_enabled then error("bare noreplay did not toggle on") end
    send(client, "/noreplay invalid", "Syntax:")
    if not must(profile:Snapshot()).is_noreplay_enabled then error("invalid argument changed noreplay state") end
    must(client.World:Configure({run_seed=191192, is_ambient_population_suppressed=true}))
    must(client.Scene:Set(scene.Navigation))
    local initial = must(client.Mission:Enter(1))
    local mission = must(profile:Snapshot())
    if not mission.is_noreplay or mission.is_replay_randomized then error("mission ignored the noreplay login toggle") end
    check(scenario.Log("PASS noreplay applied to actual mission " .. mission.mission.asset))

    send(client, "/ss mode manual", "mode set to manual")
    local before = must(client.Chat:Artifacts())
    local oldPaths = {}
    for _, artifact in ipairs(before.files) do oldPaths[artifact.path] = true end
    local dumped = send(client, "/ss dump", "Snapshot SS-")
    local snapshotID = string.match(dumped.reply, "Snapshot (SS%-%d+)")
    if not snapshotID then error("snapshot reply omitted its ID") end
    waitFor("ss dump wrote its actual report", function()
        local artifacts = must(client.Chat:Artifacts())
        for _, artifact in ipairs(artifacts.files) do
            if not oldPaths[artifact.path] and artifact.name == "report.md" and artifact.byte_size > 0
                and string.find(artifact.path, snapshotID, 1, true) then return artifact end
        end
    end, 60000)
    capture(client, "commands-focus-before-kill")
    must(client.World:RestoreHero({maximum_hit_point=1000000, hit_point=1000000}))
    local healthy = must(client.World:Snapshot())
    local enemies = {}
    for index=1,3 do
        enemies[index] = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
        must(enemies[index]:SetAI(true))
    end
    waitFor("live combat before kill", function()
        return must(client.World:Snapshot()).hero.hit_point < healthy.hero.hit_point
    end)
    send(client, "/kill extra", "Syntax:")
    for _, enemy in ipairs(enemies) do
        if must(enemy:Snapshot()).is_dead then error("invalid /kill killed a fixture") end
    end
    send(client, "/kill", "Defeating all live enemies")
    waitFor("kill defeated every tracked enemy", function()
        for _, enemy in ipairs(enemies) do
            local observed = must(enemy:Snapshot())
            if not observed.is_available or not observed.is_dead or observed.hit_point > 0 then return false end
        end
        return true
    end)
    capture(client, "commands-focus-kill-passed")
    send(client, "/victory extra", "Syntax:")
    local invalidVictory = must(client.World:Snapshot())
    if invalidVictory.boss and invalidVictory.boss.is_complete then error("invalid /victory completed the mission") end
    send(client, "/victory", "Completing the active level with victory")
    waitFor("victory completed the actual boss", function()
        local current = must(client.World:Snapshot())
        if current.game_id ~= initial.game_id then error("world changed before victory") end
        return current.boss and current.boss.is_complete and current.boss.leader_object_id ~= 0
    end)
    capture(client, "commands-focus-victory-complete")
    local beam = must(client.Mission:BeamOut())
    if not beam.world.is_victory_committed then error("victory did not commit on Return to Ship") end
    waitFor("native ship results after victory", function()
        local deployment = must(client.Mission:SnapshotDeployment()).deployment_state
        return deployment and deployment.is_available and deployment.is_loaded and deployment.is_results_screen
    end, 90000)
    capture(client, "commands-focus-passed")
    check(scenario.Log("PASS ALL focused native slash commands; no direct command dispatch"))
    return nil
end
