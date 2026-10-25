---Investigate issue 106 on a fresh isolated 5-4 run using native ground input.
---A completed run means only that this bounded route produced no candidate.
---No server teleport, camera retarget, damage command or position write is used.
---Allow the full route with --lua-timeout 10m; the default helper limit is 3m.
local must = scenario.Must
local check = scenario.Check

local mission = 20 -- 5-4; change this for another authored level.
local runSeed = 104729 -- Server RNG only; not an exact replay of client layout RNG.
local isAmbientPopulationSuppressed = true -- First isolate navigation; set false for ordinary combat population.
local checkpoints = {
    {
        name = "factory-stop", position = {x = -862.031, y = -52.450, z = 2.551},
        probe = {x = -859.247, y = -48.852, z = 2.086},
    },
    {name = "boss-approach", position = {x = 1031.609, y = -263.573, z = 0.590}},
}
local offsets = {{x = 6, y = 0}, {x = -6, y = 0}, {x = 0, y = 6}, {x = 0, y = -6}}

local function horizontalDistance(first, second)
    local x, y = first.x - second.x, first.y - second.y
    return math.sqrt(x * x + y * y)
end

local function capture(run, label)
    local artifact, captureError = run.client.Diagnostics:Capture(label)
    if captureError ~= nil then
        error(label .. ": incomplete capture; " .. tostring(captureError)
            .. "; manifest=" .. tostring(artifact ~= nil and artifact.manifest_path or "unavailable"))
    end
    return artifact.manifest_path
end

local function stop(run, label, reason)
    local path = capture(run, label)
    error(reason .. "; retained owned client; manifest=" .. path)
end

local function observe(run)
    if scenario.Time() > run.deadline then
        stop(run, "camera-route-timeout", "bounded camera route exceeded ten minutes")
    end
    local worldSS = must(run.client.World:SnapshotLiving())
    if worldSS.game_id ~= run.initial.game_id or worldSS.user_id ~= run.initial.user_id
        or worldSS.instance_id ~= run.initial.instance_id or worldSS.pid ~= run.initial.pid
        or worldSS.peer_generation ~= run.initial.peer_generation
        or worldSS.transport_generation ~= run.initial.transport_generation
        or worldSS.zone_generation ~= run.initial.zone_generation then
        stop(run, "camera-epoch-changed", "world identity changed; samples cannot be compared")
    end
    local receipt = must(run.client.World:SnapshotNative())
    local nativeSS = receipt.gameplay_state
    if not receipt.is_processed or nativeSS == nil or not nativeSS.is_available
        or not nativeSS.is_world_active or nativeSS.position == nil or nativeSS.controlled_handle == nil then
        stop(run, "camera-native-unavailable", "no processed living native hero observation")
    end
    local cameraSS = nativeSS.camera
    if cameraSS == nil or not cameraSS.is_available then
        stop(run, "camera-observer-unavailable", "this scenario requires the matching camera-observer Fang build")
    end
    local now = scenario.Time()
    local isFollowing = not cameraSS.is_override_active and cameraSS.mode == 5
        and cameraSS.follow_mode == 4 and cameraSS.zoom_mode == 3
    -- These are investigation thresholds, not recovered camera tolerances.
    -- A candidate captures evidence and stops; it does not assert a game bug.
    local candidate = nil
    if isFollowing and cameraSS.target_handle ~= nativeSS.controlled_handle then
        candidate = "camera-target-candidate"
    elseif isFollowing and cameraSS.is_anchor_available and cameraSS.center ~= nil
        and cameraSS.source_position ~= nil
        and horizontalDistance(cameraSS.center, cameraSS.source_position) > 4 then
        candidate = "camera-lag-candidate"
    end
    if candidate ~= nil then
        if run.candidate ~= candidate then
            run.candidate, run.candidateAt = candidate, now
        elseif now - run.candidateAt >= 2000 then
            stop(run, candidate, "sustained native camera mismatch; review cinematic and smoothing evidence")
        end
    else
        run.candidate, run.candidateAt = nil, nil
    end
    local isSameHero = run.previousHandle == nativeSS.controlled_handle
    local isStationary = isSameHero and run.previousPosition ~= nil
        and horizontalDistance(run.previousPosition, nativeSS.position) < 0.05
    local poseDistance = horizontalDistance(worldSS.hero.position, nativeSS.position)
    if isStationary and isFollowing and poseDistance > 4 then
        run.poseMismatchAt = run.poseMismatchAt or now
        if now - run.poseMismatchAt >= 2000 then
            stop(run, "camera-pose-candidate", "stationary hero differs horizontally from server by "
                .. string.format("%.3f", poseDistance) .. " units; camera failure is not established")
        end
    else
        run.poseMismatchAt = nil
    end
    run.previousPosition, run.previousHandle = nativeSS.position, nativeSS.controlled_handle
    return worldSS, nativeSS
end

local function observeFor(run, duration)
    local deadline = scenario.Time() + duration
    repeat
        observe(run)
        check(scenario.Sleep(200)) -- Sampling interval, never readiness evidence.
    until scenario.Time() >= deadline
end

local function pathTo(run, origin, destination, isPrefixAllowed)
    local path = must(run.client.World:Path(origin, destination))
    local reason = path.unavailable_reason or "no complete navigation result"
    if not path.is_available or not path.is_complete or path.is_truncated then
        -- A bounded output prefix can guide the next short native step. It
        -- never establishes arrival; the next iteration queries a fresh path.
        if isPrefixAllowed and path.is_truncated and #path.points >= 2 then
            return path
        end
        return nil, string.format("%s; from=(%.3f,%.3f,%.3f) to=(%.3f,%.3f,%.3f)",
            reason, origin.x, origin.y, origin.z, destination.x, destination.y, destination.z)
    end
    return path
end

local function clickGround(run, point, isTraversal)
    local receipt, actionError
    local controlledHandle = run.previousHandle
    local eligibilityDeadline = scenario.Time() + 5000
    repeat
        receipt, actionError = run.client.World:Click(point)
        if actionError == nil then
            break
        end
        local click = receipt ~= nil and receipt.gameplay_click or nil
        local nativeSS = receipt ~= nil and receipt.gameplay_state or nil
        -- The native adapter reports unavailable before a consumed press;
        -- interrupted consumed presses instead report outcome_unknown. Only
        -- this explicit refusal permits waiting and another guarded request.
        local isRefusedBeforePress = receipt ~= nil and receipt.status == "unavailable"
            and receipt.instance_id == run.initial.instance_id and receipt.pid == run.initial.pid
            and click ~= nil and click.is_press_poll_observed == false and click.is_release_poll_observed == false
            and nativeSS ~= nil and nativeSS.is_available and nativeSS.is_world_active
            and nativeSS.controlled_handle == controlledHandle
        if not isRefusedBeforePress then
            stop(run, "camera-input-unknown", "native input has unknown outcome; no replay: " .. tostring(actionError))
        end
        if scenario.Time() >= eligibilityDeadline then
            stop(run, "camera-input-blocked", "native guards refused the click before any press for five seconds: "
                .. tostring(actionError))
        end
        observeFor(run, 400)
    until false
    local click = receipt ~= nil and receipt.gameplay_click or nil
    if receipt == nil or not receipt.is_processed or click == nil
        or not click.is_projected or not click.is_press_poll_observed or not click.is_release_poll_observed then
        stop(run, "camera-ground-unproven", "input omitted paired processing evidence; no replay; processed="
            .. tostring(receipt ~= nil and receipt.is_processed) .. "; projected="
            .. tostring(click ~= nil and click.is_projected) .. "; press="
            .. tostring(click ~= nil and click.is_press_poll_observed) .. "; release="
            .. tostring(click ~= nil and click.is_release_poll_observed))
    end
    if click.retained_handle == nil or (click.is_native_ground_query_success and click.chosen_ground_point == nil) then
        stop(run, "camera-click-unsupported", "processed click omitted its native target or successful ground point")
    end
    if click.retained_handle ~= 0 or not click.is_native_ground_query_success then
        local reason = "processed click selected target=" .. tostring(click.retained_handle)
            .. "; groundQuery=" .. tostring(click.is_native_ground_query_success)
            .. string.format("; requested=(%.3f,%.3f,%.3f)", point.x, point.y, point.z)
        if isTraversal then
            observeFor(run, 600)
            return nil, reason
        end
        stop(run, "camera-ground-missed", reason)
    end
    return click.chosen_ground_point
end

local function stepPoint(run, origin, path, maximumLength)
    local waypoint = nil
    for _, point in ipairs(path.points) do
        if horizontalDistance(origin, point) > 0.75 then
            waypoint = point
            break
        end
    end
    if waypoint == nil then
        return nil
    end
    local x, y, z = waypoint.x - origin.x, waypoint.y - origin.y, waypoint.z - origin.z
    local distance = math.sqrt(x * x + y * y + z * z)
    for _, length in ipairs({6, 3, 1.5}) do
        local fraction = math.min(math.min(length, maximumLength or 6) / distance, 1)
        local point = {x = origin.x + x * fraction, y = origin.y + y * fraction, z = origin.z + z * fraction}
        local receipt = must(run.client.World:Project(point))
        if receipt.projection ~= nil and receipt.projection.is_available and receipt.projection.is_on_screen then
            return point
        end
    end
    return nil
end

local routeTurns = {
    {x = 1, y = 0}, {x = 0.7071, y = 0.7071}, {x = 0.7071, y = -0.7071},
    {x = 0, y = 1}, {x = 0, y = -1}, {x = -0.7071, y = 0.7071},
    {x = -0.7071, y = -0.7071}, {x = -1, y = 0},
}

local function routeCell(position)
    return tostring(math.floor(position.x / 4)) .. ":" .. tostring(math.floor(position.y / 4))
        .. ":" .. tostring(math.floor(position.z / 3))
end

local function isObstacleSearchUnavailable(reason)
    return reason ~= nil and string.find(reason, "navigation obstacle route unavailable", 1, true) ~= nil
end

-- This is bounded scenario exploration, not a replacement gameplay pathfinder.
-- Every candidate must have a complete route through the real obstacle overlay.
local function localRoute(run, origin, destination, visitedCells)
    local distance = horizontalDistance(origin, destination)
    if distance < 0.01 then
        return nil
    end
    local directionX, directionY = (destination.x - origin.x) / distance, (destination.y - origin.y) / distance
    local bestPath, bestScore = nil, math.huge
    for _, length in ipairs({6, 12, 24}) do
        for _, turn in ipairs(routeTurns) do
            local point = {
                x = origin.x + (directionX * turn.x - directionY * turn.y) * length,
                y = origin.y + (directionY * turn.x + directionX * turn.y) * length,
                z = origin.z,
            }
            local path = pathTo(run, origin, point)
            if path ~= nil and path.goal ~= nil and path.goal.position ~= nil then
                local goal = path.goal.position
                local isNewCell = not visitedCells[routeCell(goal)]
                if isNewCell and horizontalDistance(origin, goal) >= 4 and path.goal.distance <= 3 then
                    local score = horizontalDistance(goal, destination)
                    if score < bestScore then
                        bestPath, bestScore = path, score
                    end
                end
            end
        end
    end
    return bestPath
end

local function walk(run, destination, timeout, tunnelDestination)
    local deadline = scenario.Time() + timeout
    local localDestination = nil
    local visitedCells = {}
    local routeOrigins = {}
    local localRouteCount = 0
    local isLocalRouting = false
    local clickMissCount = 0
    local missedPoint = nil
    while scenario.Time() < deadline do
        local worldSS, nativeSS = observe(run)
        if tunnelDestination ~= nil and horizontalDistance(nativeSS.position, tunnelDestination) < 5
            and math.abs(nativeSS.position.z - tunnelDestination.z) < 3 then
            return true
        end
        if horizontalDistance(nativeSS.position, destination) <= 1.25
            and math.abs(nativeSS.position.z - destination.z) < 3 then
            observeFor(run, 2500)
            return true
        end
        if localDestination ~= nil and horizontalDistance(nativeSS.position, localDestination) <= 1.25 then
            visitedCells[routeCell(localDestination)] = true
            localDestination = nil
        end
        local path, pathReason
        if not isLocalRouting or localDestination ~= nil or horizontalDistance(nativeSS.position, destination) < 20 then
            path, pathReason = pathTo(run, nativeSS.position, localDestination or destination, true)
        end
        if path == nil and localDestination == nil and (isLocalRouting or isObstacleSearchUnavailable(pathReason)) then
            isLocalRouting = true
            visitedCells[routeCell(nativeSS.position)] = true
            localRouteCount = localRouteCount + 1
            if localRouteCount > 128 then
                stop(run, "camera-local-route-exhausted", "reported region not reached after 128 validated local routes and backtracks")
            end
            path = localRoute(run, nativeSS.position, destination, visitedCells)
            if path == nil then
                -- A greedy approach can enter a cul-de-sac. Return along an
                -- observed branch before trying its remaining neighbors.
                -- Re-query the reverse path; forward travel is no proof that
                -- reverse terrain and obstacle traversal are possible.
                local origin = table.remove(routeOrigins)
                if origin == nil then
                    stop(run, "camera-local-route-blocked", "bounded local exploration exhausted every reached branch; global route was unavailable")
                end
                path, pathReason = pathTo(run, nativeSS.position, origin)
                if path == nil then
                    stop(run, "camera-backtrack-blocked", "no complete return route to the previous observed branch; " .. tostring(pathReason))
                end
            else
                routeOrigins[#routeOrigins + 1] = {
                    x = nativeSS.position.x, y = nativeSS.position.y, z = nativeSS.position.z,
                }
            end
            localDestination = path.goal.position
        end
        if path == nil then
            return false, worldSS, pathReason
        end
        local point = stepPoint(run, nativeSS.position, path, 6 / (2 ^ clickMissCount))
        if point == nil then
            stop(run, "camera-path-offscreen", "next installed path segment cannot be projected into this camera")
        end
        if missedPoint ~= nil and horizontalDistance(point, missedPoint) < 0.25 then
            stop(run, "camera-ground-missed", "shorter navigation aim did not produce a different point; no replay")
        end
        local groundPoint, clickReason = clickGround(run, point, true)
        if groundPoint == nil then
            missedPoint = point
            clickMissCount = clickMissCount + 1
            if clickMissCount >= 3 then
                stop(run, "camera-ground-missed", "three processed navigation aims missed ground; " .. clickReason)
            end
        else
            clickMissCount = 0
            missedPoint = nil
            local initialPosition = nativeSS.position
            local stepDeadline = scenario.Time() + 5000
            local isProgressObserved = false
            repeat
                check(scenario.Sleep(200))
                local currentWorldSS, currentNativeSS = observe(run)
                if horizontalDistance(currentNativeSS.position, initialPosition) > 0.25 then
                    isProgressObserved = true
                end
                if horizontalDistance(currentNativeSS.position, groundPoint) < 0.75 then
                    break
                end
                -- Ordinary tunnel contact may replace the route; observe the jump
                -- instead of issuing another action at the previous source point.
                if horizontalDistance(currentNativeSS.position, initialPosition) > 15 then
                    break
                end
            until scenario.Time() >= stepDeadline
            if not isProgressObserved then
                stop(run, "camera-route-stall", "processed ground input produced no native travel; capture is not camera proof")
            end
        end
    end
    stop(run, "camera-walk-timeout", "route did not arrive within its bounded interval")
end

local function canAttemptPortalApproach(run, origin, destination)
    local path, reason = pathTo(run, origin, destination)
    -- An unavailable long obstacle search permits choosing a candidate portal,
    -- never assuming arrival. walk() must prove each short segment afterward.
    return path ~= nil or isObstacleSearchUnavailable(reason)
end

local function reachCheckpoint(run, destination)
    for _ = 1, 8 do
        local isReached, worldSS, pathReason = walk(run, destination, 180000)
        if isReached then
            return
        end
        -- Traverse published active portals through ordinary native input,
        -- including the security/boss portal leading to the second report.
        -- Never invent a server teleport or bypass an inactive gate.
        local route = nil
        local bestDistance = math.huge
        local currentWorldSS, nativeSS = observe(run)
        for _, candidate in ipairs(worldSS.teleporters) do
            local key = tostring(candidate.marker_id) .. ":" .. tostring(candidate.destination_marker_id)
            if (candidate.is_tunnel or candidate.is_security) and candidate.is_activation_available
                and candidate.is_active and not run.attemptedRoutes[key]
                and canAttemptPortalApproach(run, nativeSS.position, candidate.source)
                and pathTo(run, candidate.destination, destination) ~= nil then
                local distance = horizontalDistance(nativeSS.position, candidate.source)
                if distance < bestDistance then
                    route, bestDistance = candidate, distance
                end
            end
        end
        if route == nil then
            stop(run, "camera-region-unreachable", "reported region has no observed path or active connecting portal; "
                .. tostring(pathReason))
        end
        local key = tostring(route.marker_id) .. ":" .. tostring(route.destination_marker_id)
        run.attemptedRoutes[key] = true
        if not walk(run, route.source, 90000, route.destination) then
            stop(run, "camera-tunnel-unreachable", "published tunnel source is not reachable through native movement")
        end
        local transitDeadline = scenario.Time() + 15000
        local isTransitObserved = false
        repeat
            local transitWorldSS, transitNativeSS = observe(run)
            if horizontalDistance(transitNativeSS.position, route.destination) < 5
                and math.abs(transitNativeSS.position.z - route.destination.z) < 3 then
                isTransitObserved = true
                break
            end
            check(scenario.Sleep(200))
        until scenario.Time() >= transitDeadline
        if not isTransitObserved then
            stop(run, "camera-tunnel-not-traversed", "ordinary tunnel input did not establish traversal; no replay")
        end
    end
    stop(run, "camera-route-exhausted", "reported region was not reached within eight ordinary tunnel attempts")
end

local function exerciseRegion(run, name, probe)
    capture(run, name .. "-before")
    if probe ~= nil then
        -- Only approach via the authoritative report pose. The known displaced
        -- native pose is an intentional ground-input probe, not a nav endpoint.
        local receipt = must(run.client.World:Project(probe))
        if receipt.projection == nil or not receipt.projection.is_available or not receipt.projection.is_on_screen then
            stop(run, "camera-probe-offscreen", "reported displaced ground point is not visible from the authoritative region")
        end
        clickGround(run, probe)
        observeFor(run, 3000)
        capture(run, name .. "-displaced-probe")
    end
    local movementCount = 0
    for pass = 1, 3 do
        local worldSS, nativeSS = observe(run)
        local origin = nativeSS.position
        for _, offset in ipairs(offsets) do
            local currentWorldSS, currentNativeSS = observe(run)
            local goal = {x = origin.x + offset.x, y = origin.y + offset.y, z = origin.z}
            local path = pathTo(run, currentNativeSS.position, goal)
            if path ~= nil then
                local point = stepPoint(run, currentNativeSS.position, path)
                if point ~= nil then
                    clickGround(run, point)
                    observeFor(run, 300)
                    local movingWorldSS, movingNativeSS = observe(run)
                    if horizontalDistance(movingNativeSS.position, currentNativeSS.position) > 0.25 then
                        movementCount = movementCount + 1
                    end
                    -- Deliberately reverse a known processed move, not replay an
                    -- unknown command. Each edge is ordinary native ground input.
                    local projection = must(run.client.World:Project(origin)).projection
                    if projection ~= nil and projection.is_available and projection.is_on_screen then
                        clickGround(run, origin)
                        observeFor(run, 2500)
                    end
                end
            end
        end
        local selection, selectionError = run.client.World:SelectHero(pass % 3)
        if selectionError ~= nil then
            stop(run, "camera-switch-unavailable", "hero switch did not complete; no replay: " .. tostring(selectionError))
        end
        if selection == nil then
            stop(run, "camera-switch-unproven", "hero switch omitted its completion result; no replay")
        end
        observeFor(run, 2500)
        capture(run, name .. "-switch-" .. tostring(pass))
    end
    if movementCount < 3 then
        stop(run, "camera-region-not-exercised", "fewer than three native movement segments observed in " .. name)
    end
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "camera-follow", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 21,
    }))
    local client = must(profile:Launch({client_id = "camera-follow", launch_mode = "normal"}))
    check(client:SetTimeout(60000))
    check(client.Cinematic:SetSkipEnabled(false))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = runSeed, is_ambient_population_suppressed = isAmbientPopulationSuppressed}))
    must(client.Scene:Set(scene.Navigation))
    local worldSS = must(client.Mission:Enter(mission))
    local run = {client = client, initial = worldSS, deadline = scenario.Time() + 600000, attemptedRoutes = {}}
    observeFor(run, 2500)
    capture(run, "camera-entry")
    -- Establish movement/switch coverage even if the installed layout cannot
    -- provide a bounded route to a historical report coordinate.
    exerciseRegion(run, "camera-spawn")
    for _, checkpoint in ipairs(checkpoints) do
        reachCheckpoint(run, checkpoint.position)
        exerciseRegion(run, checkpoint.name, checkpoint.probe)
    end
    capture(run, "camera-route-complete")
    -- Keep the isolated client available for manual comparison. Completion is
    -- bounded negative evidence, never proof that intermittent desync is fixed.
    return nil
end
