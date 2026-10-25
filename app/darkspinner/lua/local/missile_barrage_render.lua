---Automatic Missile Barrage rendering investigation for issue 117, mission 1-4.
---One guarded native cast targets an AI-disabled gunner. Do not provide manual
---input during recording. The supported semantic observer establishes actual
---acceptance and burst completion; Power changes are not used as cast evidence.
---No camera repair, terrain-render assertion, native input shortcut, or replay.
---The fresh hero and native layout are not an exact replay of the level-24 report.
local must = scenario.Must
local check = scenario.Check
local recordingDuration = 12000

local function announce(message)
    -- Older launcher binaries can still run the recording after a disk reload.
    if scenario.Log ~= nil then
        check(scenario.Log(message))
    end
end

local function capture(client, label)
    local artifact, captureError = client.Diagnostics:Capture(label)
    if captureError ~= nil then
        error(label .. ": incomplete capture; " .. tostring(captureError)
            .. "; manifest=" .. tostring(artifact ~= nil and artifact.manifest_path or "unavailable"))
    end
    return artifact.manifest_path
end

local function stop(run, label, reason)
    local manifest = capture(run.client, label)
    error(reason .. "; owned client retained for inspection; manifest=" .. manifest)
end

local function distance(first, second)
    local x, y, z = first.x - second.x, first.y - second.y, first.z - second.z
    return math.sqrt(x * x + y * y + z * z)
end

local function observe(run)
    local worldSS = must(run.client.World:SnapshotLiving())
    local initialWorldSS = run.initialWorldSS
    if worldSS.instance_id ~= initialWorldSS.instance_id or worldSS.pid ~= initialWorldSS.pid
        or worldSS.user_id ~= initialWorldSS.user_id or worldSS.game_id ~= initialWorldSS.game_id
        or worldSS.peer_generation ~= initialWorldSS.peer_generation
        or worldSS.transport_generation ~= initialWorldSS.transport_generation
        or worldSS.zone_generation ~= initialWorldSS.zone_generation
        or worldSS.hero.object_id ~= initialWorldSS.hero.object_id or worldSS.hero.index ~= 0 then
        stop(run, "missile-owner-changed", "recording lost the original deployed SRS-42")
    end
    local receipt = must(run.client.World:SnapshotNative())
    local nativeSS = receipt.gameplay_state
    if not receipt.is_processed or nativeSS == nil or not nativeSS.is_available
        or not nativeSS.is_world_active or nativeSS.position == nil
        or nativeSS.controlled_handle == nil then
        stop(run, "missile-native-unavailable", "fresh native gameplay observation unavailable")
    end
    if run.controlledHandle == nil then
        run.controlledHandle = nativeSS.controlled_handle
    elseif nativeSS.controlled_handle ~= run.controlledHandle then
        stop(run, "missile-actor-changed", "native controlled hero changed during recording")
    end
    local cameraSS = nativeSS.camera
    if cameraSS == nil or not cameraSS.is_available then
        stop(run, "missile-camera-unavailable", "native camera observer unavailable")
    end
    local isFollowing = not cameraSS.is_override_active and cameraSS.mode == 5
        and cameraSS.follow_mode == 4 and cameraSS.zoom_mode == 3
    local candidate = nil
    if isFollowing and cameraSS.target_handle ~= nativeSS.controlled_handle then
        candidate = "camera follow target differs from controlled hero"
    elseif isFollowing and cameraSS.is_anchor_available and cameraSS.center ~= nil
        and cameraSS.source_position ~= nil and distance(cameraSS.center, cameraSS.source_position) > 4 then
        candidate = "camera center differs from native follow source by more than four units"
    end
    -- Investigation threshold only; camera smoothing/cinematics require review.
    if candidate ~= nil then
        if run.candidate ~= candidate then
            run.candidate, run.candidateAt = candidate, scenario.Time()
        elseif scenario.Time() - run.candidateAt >= 2000 then
            stop(run, "missile-camera-candidate", candidate .. " for two seconds; not a terrain-render diagnosis")
        end
    else
        run.candidate, run.candidateAt = nil, nil
    end
    return worldSS
end

local function cast(run)
    run.castResult, run.castError = run.client.Ability:Cast(run.targetPosition, 60000)
    run.isCastFinished = true
end

local function record(run)
    local deadline = scenario.Time() + 60000
    local acceptedAt = nil
    local screenshotOffsets = {0, 500, 2000, 5000, 9000}
    local screenshotIndex = 1
    while scenario.Time() < deadline do
        observe(run)
        if acceptedAt == nil then
            local abilitySS = must(run.client.Ability:Snapshot())
            local castSS = abilitySS.state.cast
            if castSS ~= nil and castSS.revision == run.nextRevision
                and castSS.actor_object_id == run.initialWorldSS.hero.object_id
                and castSS.ability_id == run.abilityID and castSS.is_accepted then
                acceptedAt = scenario.Time()
                deadline = acceptedAt + recordingDuration + 1000
            elseif run.isCastFinished then
                return
            end
        end
        if acceptedAt ~= nil then
            local elapsed = scenario.Time() - acceptedAt
            local offset = screenshotOffsets[screenshotIndex]
            if offset ~= nil and elapsed >= offset then
                must(run.client.UI:Screenshot("missile-after-accept-" .. tostring(screenshotIndex)))
                screenshotIndex = screenshotIndex + 1
            end
            if elapsed >= recordingDuration then
                return
            end
        end
        check(scenario.Sleep(200))
    end
    error("Missile Barrage recording exceeded its bounded acceptance window; no replay")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "missile-render", hero_nouns = {3524487169, 1667741389, 3618328019}, chain_progression = 3,
    }))
    local client = must(profile:Launch({client_id = "missile-render", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    local initialWorldSS = must(client.Mission:Enter(4))
    local run = {client = client, initialWorldSS = initialWorldSS}
    local npc = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local npcSS = must(npc:Snapshot())
    if not npcSS.is_available or npcSS.is_ai_enabled or npcSS.is_dead or npcSS.hit_point <= 0 then
        stop(run, "missile-target-unavailable", "spawned gunner is not a living AI-disabled target")
    end
    observe(run)
    -- Publication and projection do not establish visibility; confirm the
    -- gunner in the baseline screenshot when reviewing the recording.
    local projection = must(client.World:Project(npcSS.position))
    if projection.projection == nil or not projection.projection.is_available
        or not projection.projection.is_on_screen then
        stop(run, "missile-target-off-screen", "spawned gunner has no on-screen native projection")
    end
    local abilitySS = must(client.Ability:Snapshot())
    if not abilitySS.state.is_available or not abilitySS.state.is_supported
        or abilitySS.state.ability_asset ~= "MissileTempestActive" then
        stop(run, "missile-unsupported", "this launcher lacks supported SRS-42 Missile Barrage cast observation")
    end
    run.abilityID = abilitySS.state.ability_id
    run.nextRevision = abilitySS.state.last_revision + 1
    run.targetPosition = npcSS.position
    capture(client, "missile-before-cast")
    announce("Automatically casting Missile Barrage once at the stationary gunner. Do not provide manual input.")
    local parallelError = scenario.Parallel({
        cast = function() cast(run) end,
        record = function() record(run) end,
    })
    if parallelError ~= nil then
        stop(run, "missile-recording-failed", "cast/record operation stopped without replay: " .. tostring(parallelError))
    end
    local result = run.castResult
    local castSS = result ~= nil and result.observation ~= nil and result.observation.state.cast or nil
    if run.castError ~= nil or result == nil or not result.is_completed or castSS == nil
        or castSS.revision ~= run.nextRevision or castSS.ability_id ~= run.abilityID
        or castSS.actor_object_id ~= initialWorldSS.hero.object_id or not castSS.is_accepted
        or not castSS.is_burst_completed or castSS.expected_launch_count == nil
        or castSS.expected_launch_count <= 0 or castSS.launch_count ~= castSS.expected_launch_count
        or not castSS.is_released or castSS.is_failed then
        stop(run, "missile-cast-incomplete", "Missile Barrage completion unproven; no replay: " .. tostring(run.castError))
    end
    capture(client, "missile-after-recording")
    announce("Recording complete. Client retained for inspection; review the captures for terrain disappearance.")
    -- Completion proves the supported cast finished and recording completed,
    -- not that terrain rendered correctly. Review screenshots and saved traces.
    -- Session lifetime is independent of script outcome; leave the client alive.
    return nil
end
