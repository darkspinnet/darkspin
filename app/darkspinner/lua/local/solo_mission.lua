---Attempts ordinary solo 1-1 with squad index zero, ambient population enabled.
---All actions use guarded native input. Native frame positions
---establish current movement; server snapshots establish HP, damage, deaths,
---teleporter destinations and victory independently. Server hero pose is a
---command-time sample, not continuous interpolation of the native position.
local must = scenario.Must
local check = scenario.Check
local totalTimeout = 15 * 60 * 1000
local goalTimeout = 25000
local forwardRouteIndexes = {0, 2, 4, 6} -- security.activeRoutes forward edges

local function distanceSquared(left, right)
    local dx, dy, dz = left.x - right.x, left.y - right.y, left.z - right.z
    return dx * dx + dy * dy + dz * dz
end

local function horizontalDistanceSquared(left, right)
    local dx, dy = left.x - right.x, left.y - right.y
    return dx * dx + dy * dy
end

local function stop(run, message)
    if run.failure ~= nil then
        return nil
    end
    local worldSS = run.world
    local detail = " route=" .. tostring(run.routeOrdinal) .. " kills=" .. tostring(run.killCount)
        .. " pickup_hp_gains=" .. tostring(run.pickupHealthGainCount)
        .. " pickup_mana_gains=" .. tostring(run.pickupManaGainCount)
        .. " pickup_unproven=" .. tostring(run.unprovenPickupCount)
        .. " last_pickup=" .. tostring(run.lastPickupResult)
        .. " unattributed_fixture_progress=" .. tostring(run.unattributedFixtureProgressCount)
        .. " navigation_pick_fallback=" .. tostring(run.lastNavigationPickFallback)
    if worldSS ~= nil and worldSS.hero ~= nil then
        local position = worldSS.hero.position
        detail = detail .. " hp=" .. tostring(worldSS.hero.hit_point)
            .. " server_position=" .. tostring(position.x) .. "," .. tostring(position.y) .. "," .. tostring(position.z)
    end
    if run.position ~= nil then
        local position = run.position
        detail = detail .. " native_position=" .. tostring(position.x) .. "," .. tostring(position.y) .. "," .. tostring(position.z)
            .. " native_frame=" .. tostring(run.nativeFrame)
    end
    if run.goal ~= nil then
        local goal = run.goal
        detail = detail .. " goal=" .. goal.kind .. " target_id=" .. tostring(goal.targetID)
            .. " goal_point=" .. tostring(goal.point.x) .. "," .. tostring(goal.point.y) .. "," .. tostring(goal.point.z)
            .. " native_picked_handle=" .. tostring(goal.nativePickedHandle)
            .. " aim_height=" .. tostring(goal.aimHeight)
    end
    run.failure = message .. detail
    local capture, captureError = run.client.Diagnostics:Capture("solo-stuck")
    if captureError ~= nil then
        run.failure = run.failure .. "; diagnostic capture: " .. tostring(captureError)
    elseif capture ~= nil then
        run.failure = run.failure .. "; manifest=" .. tostring(capture.manifest_path)
    end
    return nil
end

---@generic T
---@param context string
---@param result T|nil
---@param operationError ScenarioError|nil
---@return T|nil
local function accept(run, context, result, operationError)
    if operationError ~= nil then
        return stop(run, context .. ": " .. tostring(operationError))
    end
    if result == nil then
        return stop(run, context .. " returned no result")
    end
    return result
end

local function observe(run)
    local worldSS = accept(run, "world observation", run.client.World:Snapshot())
    if worldSS == nil then
        return nil
    end
    run.world = worldSS
    if not worldSS.is_available or worldSS.hero == nil then
        return stop(run, "world unavailable: " .. tostring(worldSS.unavailable_reason))
    end
    if worldSS.game_id ~= run.gameID or worldSS.user_id ~= run.userID
        or (run.instanceID ~= nil and worldSS.instance_id ~= run.instanceID)
        or (run.pid ~= nil and worldSS.pid ~= run.pid)
        or (run.peerGeneration ~= nil and worldSS.peer_generation ~= run.peerGeneration)
        or (run.transportGeneration ~= nil and worldSS.transport_generation ~= run.transportGeneration)
        or (run.zoneGeneration ~= nil and worldSS.zone_generation ~= run.zoneGeneration) then
        return stop(run, "owned account/game/peer/zone changed")
    end
    run.peerGeneration = worldSS.peer_generation
    run.transportGeneration = worldSS.transport_generation
    run.zoneGeneration = worldSS.zone_generation
    run.instanceID = worldSS.instance_id
    run.pid = worldSS.pid
    if worldSS.hero.index ~= 0 then
        return stop(run, "active hero changed from squad index zero")
    end
    if worldSS.hero.is_defeated or worldSS.hero.hit_point <= 0 then
        return stop(run, "solo hero was defeated")
    end
    return worldSS
end

local function observeNative(run, isSettling)
    local receipt = accept(run, "native frame observation", run.client.World:SnapshotNative())
    if receipt == nil then
        return nil
    end
    if receipt.instance_id ~= run.instanceID or receipt.pid ~= run.pid then
        return stop(run, "native frame no longer belongs to the owned process")
    end
    local native = receipt.gameplay_state
    if not receipt.is_processed or native == nil or not native.is_available or not native.is_world_active
        or native.position == nil or native.controlled_handle == nil or native.controlled_handle == 0
        or native.owner_thread_id == 0 or native.frame_sequence == 0 then
        if isSettling then
            return nil
        end
        return stop(run, "recent native gameplay position is unavailable")
    end
    if (run.nativeHandle ~= nil and native.controlled_handle ~= run.nativeHandle)
        or (run.nativeOwnerThread ~= nil and native.owner_thread_id ~= run.nativeOwnerThread) then
        return stop(run, "native controlled handle or gameplay owner changed")
    end
    if run.nativeFrame ~= nil and native.frame_sequence < run.nativeFrame then
        return stop(run, "native gameplay frame sequence moved backwards")
    end
    if run.nativeFrame ~= nil and native.frame_sequence == run.nativeFrame then
        if isSettling then
            return nil
        end
        -- The readiness read and first action-loop read can share one frame.
        -- Wait only through read-only polls; no stale frame permits input.
        local deadline = scenario.Time() + 1000
        while scenario.Time() < deadline do
            local sleepError = scenario.Sleep(50)
            if sleepError ~= nil then
                return stop(run, "fresh native frame wait: " .. tostring(sleepError))
            end
            local fresh = observeNative(run, true)
            if fresh ~= nil or run.failure ~= nil then
                return fresh
            end
        end
        return stop(run, "native gameplay frame did not advance within one second; no further input will be issued")
    end
    run.nativeHandle = native.controlled_handle
    run.nativeOwnerThread = native.owner_thread_id
    run.nativeFrame = native.frame_sequence
    run.position = native.position
    return native
end

local function findNPC(worldSS, objectID)
    for _, npc in ipairs(worldSS.npcs) do
        if npc.object_id == objectID then
            return npc
        end
    end
    return nil
end

local function findRoute(worldSS, ordinal)
    local routeIndex = forwardRouteIndexes[ordinal]
    if routeIndex == nil then
        return nil
    end
    for _, teleporter in ipairs(worldSS.teleporters) do
        if teleporter.route_index == routeIndex and teleporter.is_security then
            return teleporter
        end
    end
    return nil
end

local function navigationPath(run, destination)
    local path = accept(run, "installed navigation path", run.client.World:Path(run.position, destination))
    if path == nil then
        return nil
    end
    if path.instance_id ~= run.instanceID or path.pid ~= run.pid or path.user_id ~= run.userID
        or path.game_id ~= run.gameID or path.peer_generation ~= run.peerGeneration
        or path.transport_generation ~= run.transportGeneration or path.zone_generation ~= run.zoneGeneration then
        return stop(run, "navigation path no longer matches the owned world epoch")
    end
    if not path.is_available or not path.is_complete or path.is_truncated then
        run.pathUnavailableReason = path.unavailable_reason
        return nil
    end
    if type(path.points) ~= "table" or #path.points == 0 or path.goal == nil or path.goal.position == nil then
        return stop(run, "complete navigation result omitted its projected goal or points")
    end
    local projectedGoal = path.goal.position
    if math.abs(projectedGoal.z - destination.z) > 3 or distanceSquared(projectedGoal, destination) > 3 * 3 then
        run.pathUnavailableReason = "mesh goal projection is outside the three-unit target interaction allowance"
        return nil
    end
    return path
end

local function nearbyEnemy(run, worldSS, route)
    local limitSquared = 45 * 45
    if route == nil then
        limitSquared = 100 * 100
    elseif route.is_activation_available and route.is_active then
        -- Defend against immediate actors before native ground navigation
        -- can pick one, without pursuing the former distant ambient groups.
        limitSquared = 12 * 12
    end
    local candidates = {}
    for _, npc in ipairs(worldSS.npcs) do
        local distance = distanceSquared(run.position, npc.position)
        if npc.is_published and npc.is_hostility_available and npc.is_hostile
            and not npc.is_dead and npc.hit_point > 0 and not npc.is_fixture
            and math.abs(run.position.z - npc.position.z) <= 8
            and distance <= limitSquared then
            candidates[#candidates + 1] = {npc = npc, distance = distance}
        end
    end
    table.sort(candidates, function(left, right)
        return left.distance < right.distance
    end)
    for index, candidate in ipairs(candidates) do
        if index > 8 then
            break
        end
        if candidate.distance <= 12 * 12 then
            -- A nearby visible actor can be attacked through ordinary native
            -- picking even when its center projects behind a mesh corner.
            return candidate.npc, nil
        end
        local path = navigationPath(run, candidate.npc.position)
        if run.failure ~= nil then
            return nil, nil
        end
        if path ~= nil then
            return candidate.npc, path
        end
    end
    return nil, nil
end

local function project(run, point)
    local receipt = accept(run, "native projection", run.client.World:Project(point))
    if receipt == nil then
        return nil
    end
    if not receipt.is_processed or receipt.projection == nil or not receipt.projection.is_available then
        return stop(run, "native projection is unavailable")
    end
    return receipt.projection
end

local function tryActiveCast(run, worldSS, npc)
    local ability = worldSS.ability
    if ability == nil or not ability.is_available or not ability.is_supported
        or not ability.is_gate_state_available or not ability.is_mana_cost_available
        or ability.mana_point < ability.mana_cost or ability.cooldown_remaining_ms > 0
        or ability.release_remaining_ms > 0 or distanceSquared(run.position, npc.position) > 12 * 12
        or math.abs(run.position.z - npc.position.z) > 8 then
        return false
    end
    local previousCast = ability.cast
    if previousCast ~= nil and not previousCast.is_failed
        and not (previousCast.is_accepted and previousCast.is_arrived
            and previousCast.is_arrival_processed and previousCast.is_released) then
        return false
    end
    local attempt = run.activeAttemptsByTarget[npc.object_id]
    if attempt ~= nil and npc.hit_point >= attempt.hitPoint then
        -- Even a completed blink can miss the intended victim. Another cast
        -- against this actor requires independently observed new HP loss.
        return false
    end
    if ability.user_id ~= run.userID or ability.actor_object_id ~= worldSS.hero.object_id or ability.creature_index ~= worldSS.hero.index
        or ability.game_id ~= run.gameID or ability.peer_generation ~= run.peerGeneration
        or ability.transport_generation ~= run.transportGeneration or ability.zone_generation ~= run.zoneGeneration then
        stop(run, "supported ability state does not match the current owned actor/epochs")
        return false
    end
    local projection = project(run, npc.position)
    if projection == nil or not projection.is_on_screen then
        return false
    end
    local now = scenario.Time()
    local origin = {x = run.position.x, y = run.position.y, z = run.position.z}
    run.goal = {kind = "activecast", point = npc.position, origin = origin, targetID = npc.object_id,
        bestHitPoint = npc.hit_point, bestDistance = distanceSquared(origin, npc.position),
        lastProgress = now, completedAt = now}
    if run.combatTargetID ~= npc.object_id then
        run.combatTargetID = npc.object_id
        run.lastCombatPosition = origin
    end
    if run.lastCombatProgress == nil then
        run.lastCombatProgress = now
    end
    run.activeAttemptsByTarget[npc.object_id] = {hitPoint = npc.hit_point}
    local result = accept(run, "active cast stopped; no replay", run.client.Ability:Cast(npc.position, 5000))
    if result == nil then
        return false
    end
    local abilitySS = result.observation ~= nil and result.observation.state or nil
    if abilitySS == nil then
        stop(run, "active cast omitted its supported server observation")
        return false
    end
    local cast = abilitySS.cast
    if not result.is_completed or cast == nil or not cast.is_accepted or not cast.is_arrived
        or not cast.is_arrival_processed or not cast.is_released or cast.is_failed
        or abilitySS.last_revision ~= ability.last_revision + 1
        or abilitySS.user_id ~= run.userID or abilitySS.game_id ~= run.gameID
        or abilitySS.peer_generation ~= run.peerGeneration or abilitySS.transport_generation ~= run.transportGeneration
        or abilitySS.zone_generation ~= run.zoneGeneration
        or cast.actor_object_id ~= worldSS.hero.object_id or cast.creature_index ~= worldSS.hero.index
        or cast.ability_id ~= ability.ability_id then
        stop(run, "active cast omitted exact next actor acceptance/completed-arrival/release evidence")
        return false
    end
    -- Completion can be a ground blink with no intended impact. Fresh pinned
    -- observations, not admission/mana/opaque native handles, establish progress.
    local fresh = observe(run)
    if fresh == nil or observeNative(run, false) == nil then
        return false
    end
    if fresh.hero.object_id ~= worldSS.hero.object_id then
        stop(run, "active cast changed the deployed server actor")
        return false
    end
    return true
end

local function visiblePoint(run, destination, path)
    local origin = run.position
    local waypoint = nil
    local isFinalWaypoint = false
    for index, point in ipairs(path.points) do
        if index > 1 and horizontalDistanceSquared(origin, point) > 0.75 * 0.75 then
            waypoint = point
            isFinalWaypoint = index == #path.points
            break
        end
    end
    if waypoint == nil then
        waypoint = path.points[#path.points]
        isFinalWaypoint = true
    end
    -- An attack uses the original NPC point only after the supported route's
    -- next waypoint is its endpoint. Intermediate corners remain ground goals.
    local projectedPoint = isFinalWaypoint and destination or waypoint
    local projection
    -- Camera projection can put a distant high waypoint on screen while the
    -- terrain ray hits the nearer bridge edge. Ground navigation always uses
    -- a mesh-segment step of at most six units, regardless of screen visibility.
    -- Nearby actor targeting remains the separate direct-hold branch above.
    if distanceSquared(origin, projectedPoint) <= 6 * 6 then
        projection = project(run, projectedPoint)
        if projection == nil then
            return nil
        end
        if projection.is_on_screen then
            return projectedPoint, isFinalWaypoint
        end
    end
    local distance = math.sqrt(distanceSquared(origin, waypoint))
    if distance < 0.5 then
        return stop(run, "nearby destination is outside the native camera")
    end
    -- Follow the installed path's first mesh segment, including its height,
    -- rather than interpolating toward a distant goal on a different floor.
    -- This remains an ordinary projected native input, never a position write.
    for _, stepLength in ipairs({6, 3, 1.5}) do
        local fraction = math.min(stepLength / distance, 1)
        local point = {
            x = origin.x + (waypoint.x - origin.x) * fraction,
            y = origin.y + (waypoint.y - origin.y) * fraction,
            z = origin.z + (waypoint.z - origin.z) * fraction,
        }
        projection = project(run, point)
        if projection == nil then
            return nil
        end
        if projection.is_on_screen then
            return point, false
        end
    end
    return stop(run, "no on-screen native movement step toward the chosen goal")
end

local function beginGoal(run, kind, point, npc, path, navigationGoal)
    local navigationDestination = navigationGoal ~= nil and navigationGoal.navigationDestination or point
    local aimHeight = 0
    if kind == "attack" and npc ~= nil then
        aimHeight = run.aimHeightsByTarget[npc.object_id] or 0
        -- Mission-1-1 aiming heuristic only: +1/+2 are distinct screen rays,
        -- not a claim about the native model center or collision geometry.
        point = {x = point.x, y = point.y, z = point.z + aimHeight}
    end
    local clickedPoint, isDestination
    if kind == "attack" and npc ~= nil and distanceSquared(run.position, point) <= 12 * 12
        and math.abs(run.position.z - point.z) <= 8 then
        local projection = project(run, point)
        if projection == nil then
            return false
        end
        if projection.is_on_screen then
            clickedPoint = point
            isDestination = true
        end
    end
    if clickedPoint == nil then
        if path == nil then
            path = navigationPath(run, point)
            if path == nil then
                if run.failure == nil then
                    stop(run, "no complete installed navigation route for " .. kind .. ": " .. tostring(run.pathUnavailableReason))
                end
                return false
            end
        end
        clickedPoint, isDestination = visiblePoint(run, point, path)
    end
    if clickedPoint == nil then
        return false
    end
    local origin = {x = run.position.x, y = run.position.y, z = run.position.z}
    local horizontalGoalDistance = math.sqrt(horizontalDistanceSquared(origin, clickedPoint))
    if not isDestination and horizontalGoalDistance < 0.25 then
        stop(run, "projected step has no meaningful horizontal direction")
        return false
    end
    local receipt, actionError
    local isHeldAttack = kind == "attack" and isDestination
    if isHeldAttack then
        receipt, actionError = run.client.World:Hold(clickedPoint, 1000)
    else
        receipt, actionError = run.client.World:Click(clickedPoint)
    end
    receipt = accept(run, "native input outcome unknown; action will not be replayed", receipt, actionError)
    if receipt == nil then
        return false
    end
    local click = receipt.gameplay_click
    if not receipt.is_processed or click == nil or not click.is_projected
        or not click.is_press_poll_observed or not click.is_release_poll_observed then
        stop(run, "native click omitted paired press/release processing evidence; outcome unknown")
        return false
    end
    if click.retained_handle == nil then
        stop(run, "native click does not expose its press-poll retained-handle observation; unsupported receipt")
        return false
    end
    if kind ~= "attack" and npc == nil and click.retained_handle ~= 0 then
        local fixtures = {}
        for _, fixture in ipairs(run.world.npcs) do
            if fixture.is_fixture and not fixture.is_dead and fixture.hit_point > 0
                and math.abs(origin.z - fixture.position.z) <= 8
                and distanceSquared(origin, fixture.position) <= 20 * 20 then
                fixtures[#fixtures + 1] = {objectID = fixture.object_id, hitPoint = fixture.hit_point}
                if #fixtures >= 32 then
                    break
                end
            end
        end
        local now = scenario.Time()
        run.goal = {kind = "navigationPick", point = clickedPoint, origin = origin,
            nativePickedHandle = click.retained_handle, fixtures = fixtures, completedAt = now,
            navigationDestination = navigationDestination,
            lastProgress = navigationGoal ~= nil and navigationGoal.lastProgress or now}
        if kind ~= "pickup" then
            run.combatTargetID = nil
            run.lastCombatPosition = nil
            run.lastCombatProgress = nil
        end
        if kind == "teleporter" then
            run.isRouteAttempted = true
        end
        -- Observe the already processed input only. The opaque native handle
        -- identifies none of these server fixtures and permits no replay.
        return true
    end
    local goalKind = isDestination and kind or "step"
    if kind == "reposition" or kind == "pickup" or kind == "groundEscape" then
        goalKind = "step"
    end
    local goalPoint = clickedPoint
    if goalKind == "attack" and click.retained_handle == 0 then
        -- No native target was retained during the press poll. The ordinary
        -- ray can choose nearer ground instead; do not invent an attack.
        goalKind = "step"
    elseif goalKind == "step" and kind ~= "reposition" and npc ~= nil and click.retained_handle ~= 0 then
        -- A ground-directed tap actually retained a native target. Wait for
        -- damage/death of the intended server NPC rather than ground arrival;
        -- the opaque native handle alone cannot identify that server actor.
        goalKind = "attack"
    end
    if goalKind ~= "attack" and click.is_native_ground_query_success then
        if click.chosen_ground_point == nil then
            stop(run, "successful native ground query omitted its chosen point")
            return false
        end
        goalPoint = click.chosen_ground_point
        if goalKind == "teleporter" and distanceSquared(goalPoint, point) > 2 * 2 then
            -- A native terrain ray may choose nearer ground. Only a real jump
            -- proves traversal; this result is another observed movement step.
            goalKind = "step"
        end
    end
    horizontalGoalDistance = math.sqrt(horizontalDistanceSquared(origin, goalPoint))
    local now = scenario.Time()
    run.goal = {
        kind = goalKind,
        point = goalPoint,
        origin = origin,
        horizontalGoalDistance = horizontalGoalDistance,
        bestHorizontalProgress = 0,
        targetID = npc ~= nil and npc.object_id or nil,
        bestHitPoint = npc ~= nil and npc.hit_point or nil,
        bestDistance = distanceSquared(run.position, goalPoint),
        nativePickedHandle = click.retained_handle,
        aimHeight = aimHeight,
        isReposition = kind == "reposition",
        navigationDestination = navigationDestination,
        isBlockedGround = npc == nil and kind ~= "pickup" and click.retained_handle == 0
            and click.is_native_ground_query_success and click.chosen_ground_point ~= nil
            and distanceSquared(origin, click.chosen_ground_point) < 0.75 * 0.75,
        isBlockedApproach = kind == "attack" and npc ~= nil and not isHeldAttack and goalKind == "step"
            and click.retained_handle == 0 and click.is_native_ground_query_success
            and click.chosen_ground_point ~= nil
            and distanceSquared(origin, click.chosen_ground_point) < 0.75 * 0.75,
        isBlockedReposition = kind == "reposition" and click.retained_handle == 0
            and click.is_native_ground_query_success and click.chosen_ground_point ~= nil
            and distanceSquared(origin, click.chosen_ground_point) <= 0.25 * 0.25,
        isBlockedAim = isHeldAttack and click.retained_handle == 0
            and click.is_native_ground_query_success and click.chosen_ground_point ~= nil
            and distanceSquared(origin, click.chosen_ground_point) <= 0.25 * 0.25,
        completedAt = now,
        lastProgress = navigationGoal ~= nil and navigationGoal.lastProgress or now,
    }
    if npc ~= nil then
        if run.combatTargetID ~= npc.object_id then
            run.combatTargetID = npc.object_id
            run.lastCombatPosition = origin
        end
        if run.lastCombatProgress == nil then
            run.lastCombatProgress = now
        end
    elseif kind ~= "pickup" then
        run.combatTargetID = nil
        run.lastCombatPosition = nil
        run.lastCombatProgress = nil
    end
    if kind == "teleporter" then
        run.isRouteAttempted = true
    end
    return true
end

local function escapeGround(run, goal)
    local origin = run.position
    local bucketKey = tostring(run.routeOrdinal) .. ":" .. tostring(math.floor(origin.x / 4))
        .. ":" .. tostring(math.floor(origin.y / 4)) .. ":" .. tostring(math.floor(origin.z / 4))
    local state = run.groundEscapesByBucket[bucketKey]
    if state == nil then
        local destination = goal.navigationDestination
        local dx, dy = destination.x - origin.x, destination.y - origin.y
        local distance = math.sqrt(dx * dx + dy * dy)
        if distance < 0.25 then
            return stop(run, "blocked navigation has no meaningful lateral escape direction")
        end
        state = {attemptCount = 0, origin = {x = origin.x, y = origin.y, z = origin.z},
            perpendicularX = -dy / distance, perpendicularY = dx / distance}
        run.groundEscapesByBucket[bucketKey] = state
    end
    while state.attemptCount < 2 do
        local routeAttemptCount = run.groundEscapeCountsByRoute[run.routeOrdinal] or 0
        if routeAttemptCount >= 8 then
            return stop(run, "navigation exhausted eight bounded lateral escape candidates on this route")
        end
        state.attemptCount = state.attemptCount + 1
        run.groundEscapeCountsByRoute[run.routeOrdinal] = routeAttemptCount + 1
        local side = state.attemptCount == 1 and 1 or -1
        local point = {x = state.origin.x + side * 4 * state.perpendicularX,
            y = state.origin.y + side * 4 * state.perpendicularY, z = state.origin.z}
        local path = navigationPath(run, point)
        if run.failure ~= nil then
            return false
        end
        if path ~= nil then
            -- Follow its first meaningful mesh segment, not a direct ray to
            -- the lateral endpoint that might cross the same bridge post.
            return beginGoal(run, "groundEscape", path.goal.position, nil, path, goal)
        end
    end
    return stop(run, "navigation exhausted two lateral escape candidates in this four-unit origin bucket")
end

local function reposition(run, npc)
    local state = run.repositionsByTarget[npc.object_id]
    if state == nil then
        local dx, dy = npc.position.x - run.position.x, npc.position.y - run.position.y
        local distance = math.sqrt(dx * dx + dy * dy)
        if distance < 0.25 then
            stop(run, "blocked target is too close to define a distinct lateral movement direction")
            return false
        end
        state = {
            attemptCount = 0,
            origin = {x = run.position.x, y = run.position.y, z = run.position.z},
            perpendicularX = -dy / distance,
            perpendicularY = dx / distance,
        }
        run.repositionsByTarget[npc.object_id] = state
    end
    while state.attemptCount < 2 do
        state.attemptCount = state.attemptCount + 1
        local side = state.attemptCount == 1 and 1 or -1
        local point = {
            x = state.origin.x + side * 4 * state.perpendicularX,
            y = state.origin.y + side * 4 * state.perpendicularY,
            z = state.origin.z,
        }
        -- A supported mesh path is only a candidate. The native ray and actual
        -- observed movement must still establish whether scenery permits it.
        local path = navigationPath(run, point)
        if run.failure ~= nil then
            return false
        end
        if path ~= nil then
            local projection = project(run, path.goal.position)
            if projection == nil then
                return false
            end
            if projection.is_on_screen then
                return beginGoal(run, "reposition", path.goal.position, npc, path)
            end
        end
    end
    stop(run, "blocked target exhausted two distinct lateral navigation candidates after normally processed aim misses")
    return false
end

local function pickupByID(worldSS, objectID)
    for _, pickup in ipairs(worldSS.pickups or {}) do
        if pickup.object_id == objectID then
            return pickup
        end
    end
    return nil
end

local function finishPickup(run, isResourceGainObserved, reason)
    local attempt = run.pickupAttempt
    if attempt == nil then
        return
    end
    if run.goal ~= nil and run.goal.pickupKey == attempt.key and run.goal.kind ~= "navigationPick" then
        run.goal = nil
    end
    if isResourceGainObserved then
        if attempt.kind == "health" then
            run.pickupHealthGainCount = run.pickupHealthGainCount + 1
        else
            run.pickupManaGainCount = run.pickupManaGainCount + 1
        end
    else
        run.unprovenPickupCount = run.unprovenPickupCount + 1
    end
    run.lastPickupResult = attempt.kind .. ":" .. tostring(attempt.objectID) .. ":" .. reason
    run.nextPickupAt = scenario.Time() + 2000
    run.pickupAttempt = nil
end

local function observePickup(run, worldSS)
    local attempt = run.pickupAttempt
    if attempt == nil then
        return
    end
    local resourcePoint = attempt.kind == "health" and worldSS.hero.hit_point or worldSS.hero.mana_point
    if resourcePoint > attempt.previousResourcePoint then
        -- Adjacent snapshots prove a resource increase, not its exact cause.
        -- An absent orb can instead mean expiry or a reserved pickup.
        finishPickup(run, true, attempt.kind == "health" and "observed_hp_increase" or "observed_mana_increase")
        return
    end
    attempt.previousResourcePoint = resourcePoint
    local now = scenario.Time()
    if now >= attempt.deadline then
        finishPickup(run, false, "bounded_detour_without_resource_increase")
        return
    end
    if not worldSS.is_pickup_available then
        return
    end
    local pickup = pickupByID(worldSS, attempt.objectID)
    if pickup == nil or not pickup.is_collectible or pickup.is_expired then
        if attempt.unavailableSince == nil then
            attempt.unavailableSince = now
        elseif now - attempt.unavailableSince >= 600 then
            finishPickup(run, false, "missing_or_uncollectible_without_resource_increase")
        end
    else
        attempt.unavailableSince = nil
    end
end

local function drivePickup(run, worldSS, path)
    local attempt = run.pickupAttempt
    if attempt == nil or run.goal ~= nil or not worldSS.is_pickup_available then
        return true
    end
    local pickup = pickupByID(worldSS, attempt.objectID)
    if pickup == nil or not pickup.is_collectible or pickup.is_expired then
        return true
    end
    if path == nil then
        path = navigationPath(run, pickup.position)
        if run.failure ~= nil then
            return false
        end
        if path == nil then
            finishPickup(run, false, "no_supported_navigation_route")
            return true
        end
    end
    if not beginGoal(run, "pickup", pickup.position, nil, path) then
        return false
    end
    run.goal.pickupKey = attempt.key
    return true
end

local function chooseResourcePickup(run, worldSS)
    if run.pickupAttempt ~= nil or not worldSS.is_pickup_available or worldSS.hero.maximum_hit_point <= 0
        or (run.nextPickupAt ~= nil and scenario.Time() < run.nextPickupAt)
        or (run.goal ~= nil and (run.goal.targetID ~= nil or run.goal.kind == "navigationPick")) then
        return true
    end
    local isHealthNeeded = worldSS.hero.hit_point < worldSS.hero.maximum_hit_point * 0.7
    local ability = worldSS.ability
    local isManaNeeded = ability ~= nil and ability.is_available and ability.is_supported
        and worldSS.hero.maximum_mana_point > 0 and worldSS.hero.mana_point < worldSS.hero.maximum_mana_point * 0.3
        and worldSS.hero.hit_point >= worldSS.hero.maximum_hit_point * 0.4
    if not isHealthNeeded and not isManaNeeded then
        return true
    end
    local isNearbyThreat = false
    for _, npc in ipairs(worldSS.npcs) do
        if npc.is_published and npc.is_hostility_available and npc.is_hostile
            and not npc.is_fixture and not npc.is_dead and npc.hit_point > 0
            and math.abs(run.position.z - npc.position.z) <= 8
            and distanceSquared(run.position, npc.position) < 8 * 8 then
            isNearbyThreat = true
            break
        end
    end
    local candidates = {}
    for _, pickup in ipairs(worldSS.pickups or {}) do
        local distance = distanceSquared(run.position, pickup.position)
        local key = tostring(worldSS.game_id) .. ":" .. tostring(worldSS.peer_generation) .. ":"
            .. tostring(worldSS.zone_generation) .. ":" .. tostring(pickup.object_id)
        if ((pickup.kind == "health" and isHealthNeeded) or (pickup.kind == "mana" and isManaNeeded))
            and pickup.is_collectible and not pickup.is_expired
            and pickup.is_server_registered and not pickup.is_reserved and pickup.available_in_ms <= 0
            and (pickup.expires_in_ms == 0 or pickup.expires_in_ms > 3000)
            and math.abs(run.position.z - pickup.position.z) <= 3
            and distance <= 20 * 20 and not run.attemptedPickupKeys[key]
            and (not isNearbyThreat or distance <= 4 * 4) then
            candidates[#candidates + 1] = {pickup = pickup, key = key, distance = distance,
                priority = pickup.kind == "health" and 0 or 1}
        end
    end
    table.sort(candidates, function(left, right)
        if left.priority ~= right.priority then
            return left.priority < right.priority
        end
        return left.distance < right.distance
    end)
    for index, candidate in ipairs(candidates) do
        if index > 4 then
            break
        end
        -- Known unrouteable/expired IDs are never tried again in this epoch.
        run.attemptedPickupKeys[candidate.key] = true
        local path = navigationPath(run, candidate.pickup.position)
        if run.failure ~= nil then
            return false
        end
        if path ~= nil then
            run.goal = nil -- Supersedes only an interruptible route ground goal.
            local detourMS = 6000
            if candidate.pickup.expires_in_ms > 0 then
                detourMS = math.min(detourMS, candidate.pickup.expires_in_ms - 500)
            end
            run.pickupAttempt = {
                objectID = candidate.pickup.object_id, key = candidate.key,
                kind = candidate.pickup.kind,
                previousResourcePoint = candidate.pickup.kind == "health" and worldSS.hero.hit_point or worldSS.hero.mana_point,
                deadline = scenario.Time() + detourMS,
            }
            return drivePickup(run, worldSS, path)
        end
    end
    return true
end

local function updateGoal(run, worldSS)
    local goal = run.goal
    if goal == nil then
        return true
    end
    local now = scenario.Time()
    if goal.kind == "navigationPick" then
        if horizontalDistanceSquared(run.position, goal.origin) >= 0.25 * 0.25 then
            run.goal = nil
            return true
        end
        for _, fixture in ipairs(goal.fixtures) do
            local observed = findNPC(worldSS, fixture.objectID)
            if observed ~= nil and observed.hit_point < fixture.hitPoint then
                -- This is real fixture progress, without attributing its
                -- cause or correlating the opaque selected native handle.
                run.unattributedFixtureProgressCount = run.unattributedFixtureProgressCount + 1
                run.goal = nil
                return true
            end
        end
        if now - goal.lastProgress > goalTimeout then
            stop(run, "unintended native navigation target exhausted its shared 25-second progress deadline")
            return false
        end
        if now - goal.completedAt >= 2500 then
            run.lastNavigationPickFallback = "paired_native_pick_without_movement_or_fixture_hp_loss"
                .. ":baseline_fixture_total=" .. tostring(#goal.fixtures)
                .. ":observation_ms=" .. tostring(now - goal.completedAt)
                .. ":native_horizontal_delta=" .. tostring(math.sqrt(horizontalDistanceSquared(run.position, goal.origin)))
            -- A corpse or other opaque picked actor may obstruct the ray.
            -- Try a distinct bounded mesh-guided lateral goal, never replay
            -- this input or invent a server identity for the native handle.
            return escapeGround(run, goal)
        end
        return true
    end
    local distance = distanceSquared(run.position, goal.point)
    if goal.isBlockedGround and run.pickupAttempt == nil and now - goal.completedAt >= 1000
        and distanceSquared(run.position, goal.origin) < 0.25 * 0.25 then
        if now - goal.lastProgress > goalTimeout then
            stop(run, "blocked ground navigation exhausted its shared 25-second progress deadline")
            return false
        end
        -- Paired processing plus a known near-origin ground result permits
        -- a distinct mesh-guided escape; unknown actions never reach here.
        return escapeGround(run, goal)
    end
    if goal.kind ~= "step" and distance < goal.bestDistance - 0.5 then
        goal.bestDistance = distance
        goal.lastProgress = now
    end
    if goal.targetID ~= nil then
        local npc = findNPC(worldSS, goal.targetID)
        if npc == nil then
            stop(run, "target disappeared from the bounded world snapshot; death is unproven")
            return false
        end
        if npc.is_dead and npc.hit_point <= 0 then
            run.killCount = run.killCount + 1
            run.lastCombatProgress = now
            run.bossAcquisitionProgressAt = now
            run.goal = nil
            return true
        end
        if npc.hit_point < goal.bestHitPoint then
            -- Actual intended-target damage proves action progress. A fresh
            -- paired hold can follow against the latest observed position.
            run.lastCombatProgress = now
            run.bossAcquisitionProgressAt = now
            run.goal = nil
            return true
        end
        local previous = run.lastCombatPosition
        local targetDX, targetDY = npc.position.x - previous.x, npc.position.y - previous.y
        local targetDistance = math.sqrt(targetDX * targetDX + targetDY * targetDY)
        if targetDistance > 0.25 then
            local movedDX, movedDY = run.position.x - previous.x, run.position.y - previous.y
            local towardVictim = (movedDX * targetDX + movedDY * targetDY) / targetDistance
            if towardVictim >= 0.25 then
                run.lastCombatProgress = now
                run.bossAcquisitionProgressAt = now
                run.lastCombatPosition = {x = run.position.x, y = run.position.y, z = run.position.z}
            end
        end
        -- A new target coordinate alone never resets this shared deadline.
        if now - run.lastCombatProgress > goalTimeout then
            stop(run, "combat stuck without movement toward the active victim or actual server damage/death for 25 seconds")
            return false
        end
        if goal.isBlockedApproach and now - goal.completedAt >= 1000
            and distanceSquared(run.position, goal.origin) < 0.25 * 0.25 then
            -- The approach tap resolved to known near-origin ground and
            -- produced no movement. Keep the victim and shared combat clock,
            -- and try the existing bounded mesh-guided lateral candidates.
            return reposition(run, npc)
        end
        if goal.isBlockedAim then
            if goal.aimHeight >= 2 then
                return reposition(run, npc)
            end
            -- Only an observed zero target plus a chosen point at the actor's
            -- feet permits the next distinct aim ray. Errors/unknown outcomes
            -- already stopped in beginGoal; aim changes never reset combat time.
            run.aimHeightsByTarget[goal.targetID] = goal.aimHeight + 1
            run.goal = nil
            return true
        end
        if goal.isBlockedReposition then
            return reposition(run, npc)
        end
        if goal.kind == "attack" and now - goal.completedAt >= 1500
            and distanceSquared(npc.position, goal.point) >= 3 * 3 then
            run.goal = nil
            return true
        end
        if goal.kind == "activecast" then
            -- The terminal cast was already observed. No intended damage/death
            -- means an ordinary basic goal can follow; the per-target guard
            -- prevents repeating this cast against an unchanged HP sample.
            run.goal = nil
            return true
        end
    end
    if goal.kind == "step" then
        -- A native ground query can choose the actor's current point. Keep
        -- that goal bounded by the ordinary no-progress deadline, without
        -- dividing by zero, claiming movement, or issuing another click.
        if goal.horizontalGoalDistance >= 0.25 then
            local goalDX = goal.point.x - goal.origin.x
            local goalDY = goal.point.y - goal.origin.y
            local movedDX = run.position.x - goal.origin.x
            local movedDY = run.position.y - goal.origin.y
            local horizontalProgress = (movedDX * goalDX + movedDY * goalDY) / goal.horizontalGoalDistance
            if horizontalProgress > goal.bestHorizontalProgress + 0.1 then
                goal.bestHorizontalProgress = horizontalProgress
                goal.lastProgress = now
            end
            local threshold = math.min(2.5, goal.horizontalGoalDistance * 0.5)
            local isNearPoint = horizontalDistanceSquared(run.position, goal.point) <= 2 * 2
            local isMovementObserved = horizontalDistanceSquared(run.position, goal.origin) >= 0.25 * 0.25
            if horizontalProgress >= threshold or (isNearPoint and isMovementObserved and horizontalProgress > 0) then
                -- This completes one observed movement increment, not arrival
                -- at the eventual teleporter. Replan from the native position.
                if goal.isReposition then
                    -- A changed native vantage point, not a mere click, permits
                    -- a fresh bounded aim sequence. The per-target lateral
                    -- counter and shared combat deadline are never reset here.
                    run.aimHeightsByTarget[goal.targetID] = 0
                end
                run.goal = nil
                return true
            end
        end
    end
    if now - goal.lastProgress > goalTimeout then
        stop(run, "no forward native goal progress or intended server damage/death for 25 seconds; goal=" .. goal.kind)
        return false
    end
    return true
end

local function completionObserved(run, worldSS)
    local boss = worldSS.boss
    return run.routeOrdinal > #forwardRouteIndexes and run.killCount > 0
        and boss ~= nil and boss.is_complete and boss.leader_object_id ~= 0
        and boss.leader_hit_point <= 0 and boss.is_beam_out_committed
        and worldSS.is_objective_complete and worldSS.is_victory_available and worldSS.is_victory_committed
end

local function waitBossTarget(run, worldSS)
    local now = scenario.Time()
    if run.bossAcquisitionProgressAt == nil then
        run.bossAcquisitionProgressAt = now
    end
    if now - run.bossAcquisitionProgressAt <= goalTimeout then
        -- The normal loop continues pinned World/native Snapshot observations.
        -- Publication/intro delay permits waiting, never invented navigation.
        return true
    end
    local nearestDistance, nearestElevation, nearestID
    for _, npc in ipairs(worldSS.npcs) do
        if npc.is_published and npc.is_hostility_available and npc.is_hostile
            and not npc.is_fixture and not npc.is_dead and npc.hit_point > 0 then
            local distance = math.sqrt(distanceSquared(run.position, npc.position))
            if nearestDistance == nil or distance < nearestDistance then
                nearestDistance = distance
                nearestElevation = math.abs(run.position.z - npc.position.z)
                nearestID = npc.object_id
            end
        end
    end
    local boss = worldSS.boss
    stop(run, "boss-area acquisition exhausted 25 seconds without actual movement/damage/death progress"
        .. " boss_phase=" .. tostring(boss ~= nil and boss.phase or nil)
        .. " leader_id=" .. tostring(boss ~= nil and boss.leader_object_id or nil)
        .. " npc_total=" .. tostring(worldSS.npc_total) .. " npc_truncated=" .. tostring(worldSS.is_npc_truncated)
        .. " nearest_hostile_id=" .. tostring(nearestID) .. " nearest_hostile_distance=" .. tostring(nearestDistance)
        .. " nearest_hostile_abs_dz=" .. tostring(nearestElevation)
        .. " path_unavailable=" .. tostring(run.pathUnavailableReason))
    return false
end

local function finishMission(run, worldSS)
    if not completionObserved(run, worldSS) then
        if run.isBeamOutAttempted then
            stop(run, "BeamOut was already attempted; no native action will be replayed")
            return run.failure
        end
        run.isBeamOutAttempted = true
        -- Objectives are completed during the result transaction. Boss death
        -- permits this operation; objective completion cannot precede it.
        local beam = accept(run, "BeamOut outcome unknown; action will not be replayed", run.client.Mission:BeamOut())
        if beam == nil then
            return run.failure
        end
        local result = beam.world
        if result == nil or result.instance_id ~= run.instanceID or result.pid ~= run.pid
            or result.user_id ~= run.userID or result.game_id ~= run.gameID
            or result.peer_generation ~= run.peerGeneration or result.transport_generation ~= run.transportGeneration
            or result.zone_generation ~= run.zoneGeneration then
            stop(run, "BeamOut result omitted the pinned owned world identity and epochs")
            return run.failure
        end
        if result.boss == nil or result.boss.leader_object_id ~= worldSS.boss.leader_object_id then
            stop(run, "BeamOut result changed the completed boss identity")
            return run.failure
        end
        run.world = result
        worldSS = result
    end
    if not worldSS.is_available or not completionObserved(run, worldSS) then
        stop(run, "BeamOut did not establish actual personal victory, completed objectives and committed boss beam-out")
        return run.failure
    end
    local capture = accept(run, "completion diagnostic capture", run.client.Diagnostics:Capture("solo-complete"))
    if capture == nil then
        return run.failure
    end
    return nil
end

local function waitNativeWorld(run)
    -- The arrival animation can ignore a fully processed click at frame five.
    -- This explicit allowance is not an action-eligibility assertion.
    local settleError = scenario.Sleep(1000)
    if settleError ~= nil then
        stop(run, "initial arrival allowance: " .. tostring(settleError))
        return false
    end
    local deadline = scenario.Time() + 10000
    local matchingSince = nil
    local matchingCount = 0
    local previousPosition = nil
    while scenario.Time() < deadline do
        local worldSS = observe(run)
        if worldSS == nil then
            return false
        end
        local native = observeNative(run, true)
        if run.failure ~= nil then
            return false
        end
        if native ~= nil and distanceSquared(native.position, worldSS.hero.position) <= 2 * 2
            and (previousPosition == nil or distanceSquared(native.position, previousPosition) <= 0.25 * 0.25) then
            if matchingSince == nil then
                matchingSince = scenario.Time()
            end
            matchingCount = matchingCount + 1
            -- Arrival settling is a bounded timing allowance plus repeated
            -- matching observations, not proof of native action eligibility.
            if matchingCount >= 3 and scenario.Time() - matchingSince >= 200 then
                return true
            end
        else
            matchingSince = nil
            matchingCount = 0
        end
        if native ~= nil then
            previousPosition = native.position
        end
        local sleepError = scenario.Sleep(100)
        if sleepError ~= nil then
            stop(run, "native readiness wait: " .. tostring(sleepError))
            return false
        end
    end
    stop(run, "native/server arrival positions did not settle with advancing matching gameplay polls within 10 seconds")
    return false
end

local function play(run)
    while scenario.Time() - run.startedAt <= totalTimeout do
        local worldSS = observe(run)
        if worldSS == nil then
            return run.failure
        end
        if worldSS.boss ~= nil and worldSS.boss.is_complete and worldSS.boss.leader_object_id ~= 0
            and worldSS.boss.leader_hit_point <= 0 then
            return finishMission(run, worldSS)
        end
        if observeNative(run, false) == nil then
            return run.failure
        end
        local route = findRoute(worldSS, run.routeOrdinal)
        if run.routeOrdinal <= #forwardRouteIndexes and route == nil then
            stop(run, "no observed forward security teleporter for the current 1-1 area")
            return run.failure
        end
        if route ~= nil and run.isRouteAttempted
            and distanceSquared(worldSS.hero.position, route.destination) < 20 * 20
            and distanceSquared(worldSS.hero.position, route.source) > 50 * 50
            and distanceSquared(run.position, route.destination) < 20 * 20
            and distanceSquared(run.position, route.source) > 50 * 50 then
            run.routeOrdinal = run.routeOrdinal + 1
            if run.routeOrdinal > #forwardRouteIndexes then
                -- An actual native/server portal jump establishes progress.
                run.bossAcquisitionProgressAt = scenario.Time()
            end
            run.isRouteAttempted = false
            run.goal = nil
            local capture = accept(run, "teleporter diagnostic capture", run.client.Diagnostics:Capture("solo-route-" .. tostring(run.routeOrdinal - 1)))
            if capture == nil then
                return run.failure
            end
            route = findRoute(worldSS, run.routeOrdinal)
        end
        observePickup(run, worldSS)
        if not updateGoal(run, worldSS) then
            return run.failure
        end
        if not chooseResourcePickup(run, worldSS) or not drivePickup(run, worldSS, nil) then
            return run.failure
        end
        local enemy, enemyPath
        if run.pickupAttempt == nil and (run.goal == nil or (run.goal.kind ~= "attack"
            and run.goal.kind ~= "navigationPick" and run.goal.targetID == nil)) then
            enemy, enemyPath = nearbyEnemy(run, worldSS, route)
            if run.failure ~= nil then
                return run.failure
            end
        end
        if run.pickupAttempt == nil and (run.goal == nil or (run.goal.kind ~= "attack"
            and run.goal.kind ~= "navigationPick" and run.goal.targetID == nil and enemy ~= nil)) then
            if enemy ~= nil then
                local isCastCompleted = tryActiveCast(run, worldSS, enemy)
                if run.failure ~= nil then
                    return run.failure
                end
                if not isCastCompleted and not beginGoal(run, "attack", enemy.position, enemy, enemyPath) then
                    return run.failure
                end
            elseif route ~= nil then
                if not beginGoal(run, "teleporter", route.source, nil) then
                    return run.failure
                end
            else
                if not waitBossTarget(run, worldSS) then
                    return run.failure
                end
            end
        end
        local sleepError = scenario.Sleep(200)
        if sleepError ~= nil then
            stop(run, "observation wait: " .. tostring(sleepError))
            return run.failure
        end
    end
    stop(run, "solo 1-1 exceeded the 15-minute total deadline")
    return run.failure
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "solo", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "solo", launch_mode = "normal"}))
    local run = {
        client = client, userID = profile.user_id, routeOrdinal = 1, killCount = 0,
        aimHeightsByTarget = {}, repositionsByTarget = {},
        activeAttemptsByTarget = {},
        groundEscapesByBucket = {}, groundEscapeCountsByRoute = {},
        attemptedPickupKeys = {}, pickupHealthGainCount = 0, pickupManaGainCount = 0, unprovenPickupCount = 0,
        unattributedFixtureProgressCount = 0,
    }
    if accept(run, "login", client:Login()) == nil
        or accept(run, "Arsenal readiness", client.Scene:Wait(scene.Arsenal)) == nil
        or accept(run, "server gameplay seed", client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = false})) == nil
        or accept(run, "Navigation", client.Scene:Set(scene.Navigation)) == nil
        or accept(run, "mission selection", client.Mission:Select(1)) == nil
        or accept(run, "mission start", client.Mission:Start()) == nil
        or accept(run, "deployment Continue", client.Mission:Continue()) == nil then
        return run.failure
    end
    local mission = accept(run, "dungeon entry", client.Mission:Wait())
    if mission == nil then
        return run.failure
    end
    if mission.state.user_id ~= run.userID or mission.state.mission.label ~= "1-1" then
        stop(run, "ordinary entry committed an unexpected account or mission")
        return run.failure
    end
    run.gameID = mission.state.game_id
    run.startedAt = scenario.Time()
    if not waitNativeWorld(run) then
        return run.failure
    end
    -- Server gameplay seed only; ambient population stays enabled. No native
    -- layout RNG claim, spawned actors, server damage/teleport commands, hero
    -- switches, ability shortcuts, or direct mission result writes.
    return play(run)
end
