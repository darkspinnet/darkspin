---Issue 124: mission 1-2 -> /victory -> Beam Out -> ship cinematic/voting.
---Uses the reported developer-completion path, not a combat playthrough.
---Leaves the owned client open on success or failure for visual inspection.
local must = scenario.Must
local check = scenario.Check
local transitionTimeout = 120000
local screenshotInterval = 4000

local function log(message)
    check(scenario.Log(message))
end

local function stop(client, label, reason)
    local capture, captureError = client.Diagnostics:Capture(label)
    local manifest = capture ~= nil and capture.manifest_path or "unavailable"
    error(reason .. "; retained owned client; manifest=" .. tostring(manifest)
        .. "; capture_error=" .. tostring(captureError))
end

local function capture(client, label)
    local result, captureError = client.Diagnostics:Capture(label)
    if captureError ~= nil or result == nil or not result.is_complete then
        error(label .. ": incomplete capture; " .. tostring(captureError)
            .. "; manifest=" .. tostring(result ~= nil and result.manifest_path or nil))
    end
    log(label .. ": " .. result.manifest_path)
end

local function screenshot(client, label)
    local image, imageError = client.UI:Screenshot(label)
    if imageError ~= nil or image == nil or not image.is_available or not image.is_complete then
        stop(client, "cinematic-capture-unavailable", "screenshot unavailable: " .. tostring(imageError))
    end
    return image
end

local function completeMission(client, initial)
    log("Completing mission once through the same /victory command as the report")
    must(client.UI:Press("Enter"))
    check(scenario.Sleep(250)) -- Chat-opening allowance, not delivery evidence.
    screenshot(client, "cinematic-chat-open")
    local typed = must(client.UI:Type("/victory"))
    if typed.processed_character_count ~= #"/victory" then
        stop(client, "cinematic-command-unknown", "victory text was not fully processed; no replay")
    end
    -- Preserve visible command/focus evidence before submitting. WM_CHAR
    -- processing alone does not establish that native chat consumed the text.
    screenshot(client, "cinematic-victory-typed")
    must(client.UI:Press("Enter"))
    local deadline = scenario.Time() + 20000
    repeat
        local worldSS = must(client.World:Snapshot())
        if worldSS.game_id ~= initial.game_id or worldSS.user_id ~= initial.user_id
            or worldSS.zone_generation ~= initial.zone_generation then
            stop(client, "cinematic-world-replaced", "mission identity changed before completion")
        end
        -- /victory publishes boss completion without completing dungeon objectives.
        -- Match BeamOut's actual admission guard rather than normal playthrough progress.
        if worldSS.is_available and worldSS.boss ~= nil and worldSS.boss.is_complete
            and worldSS.boss.leader_object_id ~= 0 then
            return
        end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    stop(client, "cinematic-victory-timeout", "no completed boss with a leader identity after /victory; no replay")
end

local function trackReturn(client)
    local startedAt = scenario.Time()
    local nextScreenshotAt = startedAt
    local previousState = nil
    local isActivityObserved, isMovieObserved = false, false
    local nonblankCount, screenshotCount = 0, 0
    local blackSince, blackCount = nil, 0
    repeat
        local nativeSS, snapshotError = client.Cinematic:Snapshot()
        if snapshotError ~= nil or nativeSS == nil or not nativeSS.is_available then
            stop(client, "cinematic-observer-unavailable", "native cinematic observation unavailable: "
                .. tostring(snapshotError))
        end
        local now = scenario.Time()
        local elapsed = now - startedAt
        local isSustainedBlack = false
        local isActive = nativeSS.is_active or nativeSS.is_camera_present or nativeSS.is_movie_playing
        isActivityObserved = isActivityObserved or isActive
        isMovieObserved = isMovieObserved or nativeSS.is_movie_playing
        local stateKey = table.concat({tostring(nativeSS.mode), tostring(nativeSS.game_mode),
            tostring(nativeSS.current_room), tostring(nativeSS.is_active),
            tostring(nativeSS.is_movie_playing), tostring(nativeSS.active_spline_token),
            tostring(nativeSS.is_finished_pulse)}, ":")
        if stateKey ~= previousState then
            log(string.format("Cinematic %.1fs: mode=%s game_mode=%s room=%s active=%s movie=%s spline=%s finished=%s",
                elapsed / 1000, tostring(nativeSS.mode), tostring(nativeSS.game_mode),
                tostring(nativeSS.current_room), tostring(nativeSS.is_active),
                tostring(nativeSS.is_movie_playing), tostring(nativeSS.active_spline_token),
                tostring(nativeSS.is_finished_pulse)))
            previousState = stateKey
        end
        if now >= nextScreenshotAt then
            screenshotCount = screenshotCount + 1
            local image = screenshot(client, "cinematic-frame-" .. tostring(screenshotCount))
            local pixels = image.pixels
            if pixels == nil or not pixels.is_available then
                log("Frame darkness unavailable: " .. tostring(pixels ~= nil and pixels.reason or "missing metrics"))
                blackSince, blackCount = nil, 0
            elseif pixels.near_black_fraction >= 0.995 then
                blackSince = blackSince or now
                blackCount = blackCount + 1
                -- Investigation threshold, not a recovered fade duration.
                isSustainedBlack = elapsed >= 3000 and blackCount >= 3 and now - blackSince >= 10000
            else
                blackSince, blackCount = nil, 0
                if nativeSS.is_movie_playing then
                    nonblankCount = nonblankCount + 1
                end
            end
            log(string.format("Frame %d saved: %s; near_black=%s", screenshotCount, image.image_path,
                tostring(pixels ~= nil and pixels.is_available and pixels.near_black_fraction or "unknown")))
            nextScreenshotAt = scenario.Time() + screenshotInterval
        end
        if isActivityObserved and not isActive then
            local deployment, deploymentError = client.Mission:SnapshotDeployment()
            local voting = deployment ~= nil and deployment.deployment_state or nil
            if deploymentError ~= nil then
                stop(client, "cinematic-voting-unavailable", "voting-screen observation failed: " .. tostring(deploymentError))
            end
            if voting ~= nil and voting.is_available and voting.is_loaded and voting.is_results_screen then
                capture(client, "cinematic-ship-voting")
                if not isMovieObserved or nonblankCount < 2 then
                    stop(client, "cinematic-visual-inconclusive",
                        "ship voting reached without both native movie playback and two nonblank movie samples")
                end
                log("Native cinematic completed and ship voting loaded; nonblank movie frames captured. Audio and movie content still require visual review.")
                return
            end
        end
        if isSustainedBlack then
            stop(client, "cinematic-black-candidate",
                "sustained near-black center before confirmed ship voting; review movie and fade evidence before calling this a game defect")
        end
        if not isActivityObserved and elapsed >= 20000 then
            stop(client, "cinematic-never-started", "no native camera/movie activity observed after Beam Out")
        end
        check(scenario.Sleep(500))
    until scenario.Time() - startedAt >= transitionTimeout
    stop(client, "cinematic-transition-timeout", "cinematic did not reach the ship voting screen within 120 seconds")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "mission-cinematic", hero_nouns = {454054015, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "mission-cinematic", launch_mode = "normal",
        is_intro_skipped = true, is_cinematic_skipped = false}))
    check(client:SetTimeout(60000))
    if client.Cinematic.Snapshot == nil then
        error("this scenario requires the matching Cinematic:Snapshot launcher/Fang build")
    end
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 12402, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    log("Entering mission 1-2 with in-game cinematics enabled")
    local worldSS = must(client.Mission:Enter(2))
    local missionSS = must(client.Mission:SnapshotMission())
    if missionSS.state.game_id ~= worldSS.game_id or missionSS.state.mission.label ~= "1-2" then
        stop(client, "cinematic-wrong-mission", "scenario did not enter mission 1-2")
    end
    screenshot(client, "cinematic-mission-entry")
    completeMission(client, worldSS)
    capture(client, "cinematic-before-beam-out")
    log("Invoking native Beam Out once; then observing the return-to-ship cinematic")
    local beam = must(client.Mission:BeamOut())
    if not beam.world.is_victory_available or not beam.world.is_victory_committed then
        stop(client, "cinematic-beam-uncommitted", "Beam Out did not establish personal mission completion")
    end
    check(client:SetTimeout(5000))
    trackReturn(client)
    -- The movie leads to ship Continue/Cash Out voting, not free navigation.
    -- Retain that screen for review rather than choosing the next mission.
    return nil
end
