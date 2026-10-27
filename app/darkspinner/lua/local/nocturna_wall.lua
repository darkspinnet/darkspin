---Reproduce the local 1-4 wall reports on the same native layout (0x871ba807).
---Native input only: no teleport, collision bypass, asset writes or camera edits.
local must, check = scenario.Must, scenario.Check
local target = {x = 342.063, y = 494.144, z = 40.625}
-- This exactly representable server seed derives the report's layout seed for 1-4.
local runSeed = 4417884244153018

local function distance(first, second)
    local x, y = first.x - second.x, first.y - second.y
    return math.sqrt(x * x + y * y)
end

local function observe(client)
    local worldSS = must(client.World:SnapshotLiving())
    local receipt = must(client.World:SnapshotNative())
    local nativeSS = receipt.gameplay_state
    if not receipt.is_processed or nativeSS == nil or not nativeSS.is_world_active
        or nativeSS.position == nil then
        error("native living hero observation unavailable")
    end
    return worldSS, nativeSS
end

local function capture(client, label)
    local artifact = must(client.Diagnostics:Capture(label))
    check(scenario.Log(label .. ": " .. artifact.manifest_path))
end

local function fail(client, label, reason)
    capture(client, label)
    error(reason .. "; owned client retained for inspection")
end

local function click(client, point)
    local receipt, actionError = client.World:Click(point)
    if actionError ~= nil then
        fail(client, "wall-input-unavailable", tostring(actionError) .. "; no input replay")
    end
    local input = receipt.gameplay_click
    if not receipt.is_processed or input == nil or not input.is_press_poll_observed
        or not input.is_release_poll_observed or input.retained_handle ~= 0
        or not input.is_native_ground_query_success then
        fail(client, "wall-ground-unproven", "native paired ground input not established")
    end
    return input.chosen_ground_point
end

local function walk(client, destination, timeout, tunnelDestination)
    local deadline = scenario.Time() + timeout
    local stalls = 0
    while scenario.Time() < deadline do
        local worldSS, nativeSS = observe(client)
        local origin = nativeSS.position
        if tunnelDestination ~= nil and distance(origin, tunnelDestination) < 5
            and math.abs(origin.z - tunnelDestination.z) < 3 then
            return true
        end
        if distance(origin, destination) < 1.5 and math.abs(origin.z - destination.z) < 3 then
            return true
        end
        local path = must(client.World:Path(origin, destination))
        if not path.is_available and not path.is_truncated then
            return false, path.unavailable_reason or "route unavailable", worldSS
        end
        local waypoint = nil
        for _, point in ipairs(path.points) do
            if distance(origin, point) > 0.75 then
                waypoint = point
                break
            end
        end
        if waypoint == nil then
            return false, "route has no advancing waypoint", worldSS
        end
        local length = distance(origin, waypoint)
        local fraction = math.min(5 / length, 1)
        local point = {x = origin.x + (waypoint.x - origin.x) * fraction,
            y = origin.y + (waypoint.y - origin.y) * fraction,
            z = origin.z + (waypoint.z - origin.z) * fraction}
        local projection = must(client.World:Project(point)).projection
        if projection == nil or not projection.is_on_screen then
            return false, "next route point is off screen", worldSS
        end
        local ground = click(client, point)
        local stepDeadline = scenario.Time() + 3500
        local current = origin
        repeat
            check(scenario.Sleep(250))
            local nextWorldSS, nextNativeSS = observe(client)
            current = nextNativeSS.position
            if ground ~= nil and distance(current, ground) < 0.7 then break end
            if distance(current, origin) > 15 then break end
        until scenario.Time() >= stepDeadline
        if distance(current, origin) < 0.25 then stalls = stalls + 1 else stalls = 0 end
        if stalls >= 2 then
            return false, "two distinct route queries produced no native travel", worldSS
        end
    end
    return false, "bounded traversal expired"
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "nocturna-wall",
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 3}))
    local client = must(profile:Launch({client_id = "nocturna-wall", launch_mode = "normal"}))
    check(client:SetTimeout(60000))
    check(client.Cinematic:SetSkipEnabled(false))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = runSeed, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(4))
    check(scenario.Log("Expected map-layout seed=0x871ba807; selected set=nocturna_1_AI_WandererB.Markerset; verify captured server log before comparing."))
    capture(client, "wall-entry")
    local attemptedPortals = {}
    local isReached = false
    for attempt = 1, 8 do
        local reason, worldSS
        isReached, reason, worldSS = walk(client, target, 180000)
        if isReached then break end
        capture(client, "wall-route-" .. tostring(attempt))
        local portal = nil
        for _, candidate in ipairs(worldSS ~= nil and worldSS.teleporters or {}) do
            if candidate.is_tunnel and candidate.is_active and not attemptedPortals[candidate.marker_id] then
                local path = must(client.World:Path(candidate.destination, target))
                if path.is_available and path.is_complete then portal = candidate; break end
            end
        end
        if portal == nil then fail(client, "wall-route-blocked", tostring(reason)) end
        attemptedPortals[portal.marker_id] = true
        local isTraversed, traversalReason = walk(client, portal.source, 90000, portal.destination)
        if not isTraversed then fail(client, "wall-tunnel-blocked", tostring(traversalReason)) end
        local transitDeadline = scenario.Time() + 15000
        local isTransitObserved = false
        repeat
            local transitWorldSS, transitNativeSS = observe(client)
            if distance(transitNativeSS.position, portal.destination) < 5
                and math.abs(transitNativeSS.position.z - portal.destination.z) < 3 then
                isTransitObserved = true
                break
            end
            check(scenario.Sleep(250))
        until scenario.Time() >= transitDeadline
        if not isTransitObserved then fail(client, "wall-tunnel-unproven", "native tunnel arrival not observed") end
    end
    if not isReached then fail(client, "wall-not-reached", "eight route attempts exhausted") end
    capture(client, "wall-reported-position")
    -- Across and along the near edge of marker 996624157's authored rotated box.
    for index, point in ipairs({
        {x = 338, y = 494, z = 40.625}, {x = 346, y = 494, z = 40.625},
        {x = 342, y = 499, z = 40.625}, {x = 342, y = 489, z = 40.625},
    }) do
        local beforeWorldSS, beforeNativeSS = observe(client)
        local path = must(client.World:Path(beforeNativeSS.position, point))
        check(scenario.Log(string.format("probe %d route=%s complete=%s reason=%s",
            index, tostring(path.is_available), tostring(path.is_complete), tostring(path.unavailable_reason))))
        click(client, point)
        check(scenario.Sleep(2000))
        local afterWorldSS, afterNativeSS = observe(client)
        check(scenario.Log(string.format("probe %d native=(%.3f,%.3f,%.3f) server-gap=%.3f travel=%.3f",
            index, afterNativeSS.position.x, afterNativeSS.position.y, afterNativeSS.position.z,
            distance(afterNativeSS.position, afterWorldSS.hero.position),
            distance(beforeNativeSS.position, afterNativeSS.position))))
        capture(client, "wall-probe-" .. tostring(index))
        local isReturned, returnReason = walk(client, target, 20000)
        if not isReturned then fail(client, "wall-return-blocked", tostring(returnReason)) end
    end
    check(scenario.Log("Wall probes complete; review screenshots and collision corrections, not just script completion."))
end
