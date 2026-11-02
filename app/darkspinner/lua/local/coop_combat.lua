---Two real clients playing an ambient 1-1 combat slice through native input.
---No party fixture, spawned NPC, direct damage, forced teleport, or automatic victory.
local must = scenario.Must
local check = scenario.Check
local clearedObstacleIDs = {}
local detourCells = {{},{}}
local detourCounts = {0,0}
local visitedPortalIDs = {}

local function captureBoth(clients, label)
    for _, client in ipairs(clients) do
        local capture, captureError = client.Diagnostics:Capture(label)
        check(scenario.Log(label .. " client=" .. client.ID .. " manifest="
            .. tostring(capture and capture.manifest_path) .. " error=" .. tostring(captureError)))
    end
end

local function stop(clients, message)
    error(message .. "; retained clients; no replay of unknown native input")
end

local function clickMenu(client, x, y)
    must(client.UI:Click(x, y))
    check(scenario.Sleep(600)) -- Animation allowance; observations establish outcomes.
end

local function inviteParty(clients)
    for _, client in ipairs(clients) do
        local windowSS = must(client.UI:InspectWindow()).window
        if windowSS == nil or windowSS.width ~= 1280 or windowSS.height ~= 720 then
            stop(clients, "native Social flow requires the verified 1280x720 layout")
        end
    end
    local leader, follower = clients[1], clients[2]
    clickMenu(leader, 105, 35) -- Social.
    clickMenu(leader, 368, 182) -- Lobby: exactly one other account in this owned run.
    clickMenu(leader, 930, 265) -- Invite; native callback creates the party.
    must(follower.UI:Click(604, 53)) -- Native invitation Accept, once.
    local deadline = scenario.Time() + 12000
    while scenario.Time() < deadline do
        local leaderPartySS = must(leader.Party:Snapshot()).party
        local followerPartySS = must(follower.Party:Snapshot()).party
        if leaderPartySS.id ~= 0 and leaderPartySS.id == followerPartySS.id
            and #leaderPartySS.members == 2 and #followerPartySS.members == 2 then
            clickMenu(leader, 759, 498) -- Dismiss invitation confirmation.
            clickMenu(leader, 1036, 103) -- Close Social.
            return
        end
        check(scenario.Sleep(150))
    end
    stop(clients, "native invitation did not establish the same two-member party")
end

local function sample(client, baseline)
    local worldSS = must(client.World:SnapshotLiving())
    if baseline ~= nil and (worldSS.game_id ~= baseline.game_id
        or worldSS.peer_generation ~= baseline.peer_generation
        or worldSS.transport_generation ~= baseline.transport_generation
        or worldSS.zone_generation ~= baseline.zone_generation) then
        error("co-op world ownership changed")
    end
    local nativeSS = must(client.World:SnapshotNative()).gameplay_state
    if nativeSS == nil or not nativeSS.is_world_active or nativeSS.position == nil then
        error("co-op native living hero observation unavailable")
    end
    return worldSS, nativeSS
end

local function targetByID(worldSS, objectID)
    for _, npcSS in ipairs(worldSS.npcs) do
        if npcSS.object_id == objectID then return npcSS end
    end
    return nil
end

local function distance(first, second)
    local x, y, z = first.x-second.x, first.y-second.y, first.z-second.z
    return math.sqrt(x*x+y*y+z*z)
end

local function routeLength(pathSS, origin, destination)
    local length = 0
    local previous = origin
    for _, point in ipairs(pathSS.points) do
        length = length + distance(previous, point)
        previous = point
    end
    return length + distance(previous, destination)
end

local function chooseReachableTarget(clients, baselines, excludeds)
    local firstSS, firstNativeSS = sample(clients[1], baselines[1])
    local secondSS, secondNativeSS = sample(clients[2], baselines[2])
    local selectedID, selectedLength = nil, nil
    for _, targetSS in ipairs(firstSS.npcs) do
        local remoteSS = targetByID(secondSS, targetSS.object_id)
        local minimumLength = math.max(distance(firstNativeSS.position, targetSS.position),
            distance(secondNativeSS.position, targetSS.position))
        if not excludeds[targetSS.object_id] and targetSS.is_hostility_available and targetSS.is_hostile
            and targetSS.is_published and not targetSS.is_fixture and not targetSS.is_dead
            and targetSS.hit_point > 0 and remoteSS ~= nil and not remoteSS.is_dead
            and (selectedLength == nil or minimumLength < selectedLength) then
            local firstPathSS = must(clients[1].World:Path(firstNativeSS.position, targetSS.position))
            local secondPathSS = must(clients[2].World:Path(secondNativeSS.position, targetSS.position))
            if firstPathSS.is_available and firstPathSS.is_complete and not firstPathSS.is_truncated
                and secondPathSS.is_available and secondPathSS.is_complete and not secondPathSS.is_truncated then
                local length = math.max(routeLength(firstPathSS, firstNativeSS.position, targetSS.position),
                    routeLength(secondPathSS, secondNativeSS.position, targetSS.position))
                if selectedLength == nil or length < selectedLength then
                    selectedID, selectedLength = targetSS.object_id, length
                end
            end
        end
    end
    if selectedID ~= nil then
        check(scenario.Log("Co-op selected nearest shared complete route target=" .. selectedID
            .. " longest_participant_route=" .. string.format("%.3f", selectedLength)))
        return selectedID
    end
    return nil
end

local function positionText(point)
    if point == nil then return "unavailable" end
    return string.format("(%.3f,%.3f,%.3f)", point.x, point.y, point.z)
end

local function clearRouteObstacle(clients, baselines, participant, worldSS, nativeSS, point, clickSS)
    local candidate = nil
    local candidateDistance = nil
    local remoteSS = sample(clients[3-participant], baselines[3-participant])
    for _, fixtureSS in ipairs(worldSS.npcs) do
        local remoteFixtureSS = targetByID(remoteSS, fixtureSS.object_id)
        local routeDistance = distance(fixtureSS.position, point)
        local chosenDistance = clickSS.chosen_ground_point and distance(fixtureSS.position, clickSS.chosen_ground_point)
        if fixtureSS.is_fixture and fixtureSS.is_hostility_available and fixtureSS.is_hostile
            and fixtureSS.is_published and not fixtureSS.is_dead and fixtureSS.hit_point > 0
            and remoteFixtureSS ~= nil and not remoteFixtureSS.is_dead
            and distance(nativeSS.position, fixtureSS.position) <= 14
            and routeDistance <= 8 and chosenDistance ~= nil and chosenDistance <= 8
            and (candidateDistance == nil or routeDistance < candidateDistance) then
            candidate, candidateDistance = fixtureSS, routeDistance
        end
    end
    if candidate == nil then return false end
    check(scenario.Log("Co-op clearing route fixture object=" .. candidate.object_id .. " noun=" .. candidate.noun
        .. " position=" .. positionText(candidate.position)
        .. "; correlated shared server object near blocked waypoint/chosen point; native handle identity unproven"))
    captureBoth(clients, "coop-obstacle-before-" .. candidate.object_id)
    local client = clients[participant]
    local deadline = scenario.Time()+20000
    local previousHitPoint = candidate.hit_point
    local holdCount = 0
    local isHoldAllowed = true
    while scenario.Time() < deadline do
        local firstSS = sample(clients[1], baselines[1])
        local secondSS = sample(clients[2], baselines[2])
        local firstFixtureSS = targetByID(firstSS, candidate.object_id)
        local secondFixtureSS = targetByID(secondSS, candidate.object_id)
        if firstFixtureSS == nil or secondFixtureSS == nil then
            error("route fixture left a bounded snapshot; clearance is unproven")
        end
        if firstFixtureSS.is_dead and firstFixtureSS.hit_point <= 0
            and secondFixtureSS.is_dead and secondFixtureSS.hit_point <= 0 then
            clearedObstacleIDs[candidate.object_id] = true
            captureBoth(clients, "coop-obstacle-cleared-" .. candidate.object_id)
            return true
        end
        local sourceFixtureSS = participant == 1 and firstFixtureSS or secondFixtureSS
        if firstFixtureSS.hit_point < previousHitPoint and secondFixtureSS.hit_point < previousHitPoint then
            previousHitPoint = math.max(firstFixtureSS.hit_point, secondFixtureSS.hit_point)
            isHoldAllowed = true
        end
        if isHoldAllowed and holdCount < 6 then
            local receipt = must(client.World:Hold(sourceFixtureSS.position, 1000))
            local heldClickSS = receipt.gameplay_click
            if not receipt.is_processed or heldClickSS == nil or not heldClickSS.is_press_poll_observed
                or not heldClickSS.is_release_poll_observed then
                error("route fixture attack lacks paired native processing; no replay")
            end
            holdCount = holdCount+1
            isHoldAllowed = false
        end
        check(scenario.Sleep(200))
    end
    error("route fixture clearance unproven after twenty seconds and at most six damage-confirmed holds")
end

local function detourAroundBlock(clients,baselines,participant,destination)
    local client = clients[participant]
    local worldSS,nativeSS = sample(client,baselines[participant])
    local origin = nativeSS.position
    local dx,dy = destination.x-origin.x,destination.y-origin.y
    local length = math.sqrt(dx*dx+dy*dy)
    if length < 0.1 or detourCounts[participant] >= 8 then return false end
    for _, turn in ipairs({{x=0,y=1},{x=0,y=-1},{x=-0.7071,y=0.7071},{x=-0.7071,y=-0.7071}}) do
        local candidate = {x=origin.x+(dx*turn.x-dy*turn.y)*7/length,
            y=origin.y+(dy*turn.x+dx*turn.y)*7/length,z=origin.z}
        local cell = math.floor(candidate.x/3) .. ":" .. math.floor(candidate.y/3)
        if not detourCells[participant][cell] then
            detourCells[participant][cell] = true
            local pathSS = must(client.World:Path(origin,candidate))
            local goal = pathSS.goal and pathSS.goal.position
            if pathSS.is_available and pathSS.is_complete and not pathSS.is_truncated
                and goal ~= nil and distance(goal,candidate) <= 2 then
                local projectedSS = must(client.World:Project(goal)).projection
                if projectedSS ~= nil and projectedSS.is_available and projectedSS.is_on_screen then
                    check(scenario.Log("Co-op bounded detour client=" .. client.ID .. " point=" .. positionText(goal)))
                    local receipt = must(client.World:Click(goal))
                    local clickSS = receipt.gameplay_click
                    if not receipt.is_processed or clickSS == nil or not clickSS.is_press_poll_observed
                        or not clickSS.is_release_poll_observed then error("detour input outcome unavailable; no replay") end
                    check(scenario.Sleep(1200))
                    local afterSS,afterNativeSS = sample(client,baselines[participant])
                    if distance(origin,afterNativeSS.position) >= 1 then
                        detourCounts[participant] = detourCounts[participant]+1
                        return true
                    end
                end
            end
        end
    end
    return false
end

local function approachRoute(clients, baselines, participant, objectID, isSingleStep, arrivalDistance, portalPoint)
    local client, baseline = clients[participant], baselines[participant]
    local approachDistance = arrivalDistance or 6
    local deadline = scenario.Time()+90000
    local clearanceCount = 0
    while scenario.Time() < deadline do
        local worldSS, nativeSS = sample(client, baseline)
        local targetSS = objectID ~= nil and targetByID(worldSS, objectID) or nil
        if targetSS == nil and portalPoint == nil then error("approach target left the bounded snapshot; death is unproven") end
        if targetSS ~= nil and (targetSS.is_dead or targetSS.hit_point <= 0) then return end
        local targetPosition = portalPoint or targetSS.position
        local targetDistance = distance(nativeSS.position,targetPosition)
        if targetDistance <= approachDistance then return end
        local pathSS = must(client.World:Path(nativeSS.position, targetPosition))
        if not pathSS.is_available or not pathSS.is_complete or pathSS.is_truncated then
            error("ambient enemy approach no longer has a complete observed path")
        end
        local point = nil
        for _, waypoint in ipairs(pathSS.points) do
            local separation = distance(nativeSS.position, waypoint)
            if separation > 1 then
                local fraction = math.min(7,separation,math.max(0.5,targetDistance-approachDistance))/separation
                point = {x=nativeSS.position.x+(waypoint.x-nativeSS.position.x)*fraction,
                    y=nativeSS.position.y+(waypoint.y-nativeSS.position.y)*fraction,
                    z=nativeSS.position.z+(waypoint.z-nativeSS.position.z)*fraction}
                break
            end
        end
        if point == nil then error("complete path has no actionable next waypoint") end
        check(scenario.Log("Co-op route client=" .. client.ID .. " target=" .. tostring(objectID or "portal")
            .. " native=" .. positionText(nativeSS.position) .. " server=" .. positionText(worldSS.hero.position)
            .. " waypoint=" .. positionText(point) .. " goal=" .. positionText(targetPosition)
            .. " projected_start=" .. positionText(pathSS.start and pathSS.start.position)
            .. " projected_goal=" .. positionText(pathSS.goal and pathSS.goal.position)
            .. " points=" .. tostring(pathSS.point_total) .. " layer=" .. tostring(pathSS.plan_layer)
            .. " footprint=" .. tostring(pathSS.footprint_radius)))
        local action = must(client.World:Click(point))
        local clickSS = action.gameplay_click
        if not action.is_processed or clickSS == nil or not clickSS.is_press_poll_observed
            or not clickSS.is_release_poll_observed then
            error("co-op approach input lacks paired native processing; no replay")
        end
        check(scenario.Log("Co-op route input client=" .. client.ID
            .. " ground_query=" .. tostring(clickSS.is_native_ground_query_success)
            .. " ray=" .. positionText(clickSS.ray_point)
            .. " chosen=" .. positionText(clickSS.chosen_ground_point)
            .. " retained_handle=" .. tostring(clickSS.retained_handle)))
        local isMovementObserved = false
        -- Observe the same completed input for up to three windows. A delayed
        -- native mover is not a reason to resend the click or choose a new route.
        for window=1,3 do
            check(scenario.Sleep(1000))
            local afterSS, afterNativeSS = sample(client, baseline)
            local afterTargetSS = objectID ~= nil and targetByID(afterSS, objectID) or nil
            check(scenario.Log("Co-op route observation client=" .. client.ID .. " window=" .. window
                .. " native=" .. positionText(afterNativeSS.position)
                .. " server=" .. positionText(afterSS.hero.position)
                .. " frame=" .. tostring(afterNativeSS.frame_sequence)
                .. " controlled=" .. tostring(afterNativeSS.controlled_handle)
                .. " hero_hp=" .. tostring(afterSS.hero.hit_point)
                .. " target_hp=" .. tostring(afterTargetSS and afterTargetSS.hit_point)))
            if afterTargetSS ~= nil and (afterTargetSS.is_dead or afterTargetSS.hit_point <= 0) then return end
            if distance(nativeSS.position, afterNativeSS.position) >= 0.25
                or distance(point, afterNativeSS.position) <= 1 then
                isMovementObserved = true
                break
            end
        end
        if not isMovementObserved then
            local blockedSS, blockedNativeSS = sample(client, baseline)
            local isCleared = clearanceCount < 3 and clearRouteObstacle(clients, baselines, participant,
                blockedSS, blockedNativeSS, point, clickSS)
            if isCleared then
                clearanceCount = clearanceCount+1
            elseif not detourAroundBlock(clients,baselines,participant,targetPosition) then
                error("native co-op approach blocked after observed input, fixture clearance and bounded detours")
            end
            -- A different, bounded route may follow a fully processed stopped
            -- input. Unknown input errors above never reach this branch.
        end
        if isSingleStep then return end
    end
    error("co-op approach exceeded its 90-second bound including obstacle clearance")
end

local function approach(clients, baselines, participant, objectID)
    local isComplete, approachError = pcall(approachRoute, clients, baselines, participant, objectID)
    if not isComplete then
        stop(clients, "co-op approach failed: " .. tostring(approachError))
    end
end

local function castSupported(client, baseline, objectID)
    local worldSS = sample(client, baseline)
    local targetSS = targetByID(worldSS, objectID)
    if targetSS == nil then error("ability target missing before native cast; death is unproven") end
    if targetSS.is_dead or targetSS.hit_point <= 0 then return false end
    local abilitySS = must(client.Ability:Snapshot()).state
    if not abilitySS.is_available or not abilitySS.is_supported or not abilitySS.is_gate_state_available then
        error("seeded first hero lacks a supported observable SpecialTwo ability")
    end
    local castResult = must(client.Ability:Cast(targetSS.position))
    local castSS = castResult.observation and castResult.observation.state.cast
    if not castResult.is_completed or castSS == nil or not castSS.is_accepted
        or castSS.is_failed or not castSS.is_released then
        error("co-op ability did not complete its ordinary native/server lifecycle")
    end
    check(scenario.Log("Co-op ability completed asset=" .. castSS.ability_asset
        .. " damage=" .. tostring(castSS.damage) .. "; shared HP observation follows"))
    return true
end

local function attackOnce(client, baseline, objectID)
    local beforeSS = sample(client, baseline)
    local beforeTargetSS = targetByID(beforeSS, objectID)
    if beforeTargetSS == nil then error("attack target unavailable; death is unproven") end
    if beforeTargetSS.is_dead or beforeTargetSS.hit_point <= 0 then return true end
    local receipt = must(client.World:Hold(beforeTargetSS.position, 1000))
    local clickSS = receipt.gameplay_click
    if not receipt.is_processed or clickSS == nil or not clickSS.is_press_poll_observed
        or not clickSS.is_release_poll_observed then
        error("co-op basic attack lacks paired native input processing")
    end
    local deadline = scenario.Time()+6000
    while scenario.Time() < deadline do
        local afterSS = sample(client, baseline)
        local targetSS = targetByID(afterSS, objectID)
        if targetSS ~= nil and (targetSS.is_dead or targetSS.hit_point < beforeTargetSS.hit_point) then return true end
        check(scenario.Sleep(150))
    end
    check(scenario.Log("Completed co-op attack window produced no observed damage; dodge or admission remains distinguishable"))
    return false
end

local function approachTogether(clients, baselines, objectID, arrivalDistance)
    local approachDistance = arrivalDistance or 6
    local deadline = scenario.Time()+120000
    while scenario.Time() < deadline do
        -- Alternate bounded native steps so neither teammate waits at an
        -- encounter while the other walks an entire route from spawn.
        approachRoute(clients,baselines,1,objectID,true,approachDistance)
        approachRoute(clients,baselines,2,objectID,true,approachDistance)
        local firstSS,firstNativeSS = sample(clients[1],baselines[1])
        local secondSS,secondNativeSS = sample(clients[2],baselines[2])
        local firstTargetSS = targetByID(firstSS,objectID)
        local secondTargetSS = targetByID(secondSS,objectID)
        if firstTargetSS == nil or secondTargetSS == nil then error("shared approach target unavailable") end
        if firstTargetSS.is_dead and secondTargetSS.is_dead then return end
        if distance(firstNativeSS.position,firstTargetSS.position) <= approachDistance
            and distance(secondNativeSS.position,secondTargetSS.position) <= approachDistance then return end
    end
    error("coordinated co-op approach exceeded its two-minute bound")
end

local function matchingPortal(worldSS, expected)
    for _, portalSS in ipairs(worldSS.teleporters) do
        if portalSS.object_id == expected.object_id and portalSS.route_index == expected.route_index
            and portalSS.marker_id == expected.marker_id
            and portalSS.destination_marker_id == expected.destination_marker_id
            and distance(portalSS.source,expected.source) < 0.01
            and distance(portalSS.destination,expected.destination) < 0.01
            and portalSS.is_security and not portalSS.is_tunnel and not portalSS.is_boss then
            return portalSS
        end
    end
    return nil
end

local function portalRejection(portalSS, remoteSS)
    if portalSS.object_id == 0 then return "presentation object unavailable" end
    local isRecovered = portalSS.route_index >= 0 and portalSS.marker_id == 0
    local isAuthored = portalSS.route_index == -1 and portalSS.marker_id ~= 0 and portalSS.destination_marker_id ~= 0
    if not isRecovered and not isAuthored then return "route identity unavailable" end
    if visitedPortalIDs[portalSS.object_id] then return "already traversed" end
    if not portalSS.is_security or portalSS.is_tunnel or portalSS.is_boss then return "unsupported route kind" end
    if not portalSS.is_activation_available or not portalSS.is_active then return "first peer inactive or unknown" end
    if remoteSS == nil then return "second peer route identity mismatch" end
    if not remoteSS.is_activation_available or not remoteSS.is_active then return "second peer inactive or unknown" end
    if distance(portalSS.source,portalSS.destination) < 32 then return "not a separated traversal edge" end
    return nil
end

local function portalPathText(pathSS, source)
    return "available=" .. tostring(pathSS.is_available) .. ",complete=" .. tostring(pathSS.is_complete)
        .. ",truncated=" .. tostring(pathSS.is_truncated)
        .. ",goal_distance=" .. tostring(pathSS.goal and pathSS.goal.position and distance(pathSS.goal.position,source))
        .. ",reason=" .. tostring(pathSS.unavailable_reason)
end

local function choosePortal(clients, baselines, excludeds)
    local firstSS,firstNativeSS = sample(clients[1],baselines[1])
    local secondSS,secondNativeSS = sample(clients[2],baselines[2])
    local selected,selectedLength = nil,nil
    for _, portalSS in ipairs(firstSS.teleporters) do
        local remoteSS = matchingPortal(secondSS,portalSS)
        local rejection = portalRejection(portalSS,remoteSS)
        if rejection == nil then
            local isForward = false
            for _, targetSS in ipairs(firstSS.npcs) do
                local remoteTargetSS = targetByID(secondSS,targetSS.object_id)
                if not excludeds[targetSS.object_id] and targetSS.is_hostility_available and targetSS.is_hostile
                    and targetSS.is_published and not targetSS.is_fixture and not targetSS.is_dead
                    and targetSS.hit_point > 0 and remoteTargetSS ~= nil and not remoteTargetSS.is_dead
                    and distance(portalSS.destination,targetSS.position)+32
                        < math.min(distance(firstNativeSS.position,targetSS.position),distance(secondNativeSS.position,targetSS.position)) then
                    isForward = true
                    break
                end
            end
            if isForward then
                local firstPathSS = must(clients[1].World:Path(firstNativeSS.position,portalSS.source))
                local secondPathSS = must(clients[2].World:Path(secondNativeSS.position,portalSS.source))
                if firstPathSS.is_available and firstPathSS.is_complete and not firstPathSS.is_truncated
                    and secondPathSS.is_available and secondPathSS.is_complete and not secondPathSS.is_truncated
                    and firstPathSS.goal ~= nil and secondPathSS.goal ~= nil
                    and firstPathSS.goal.position ~= nil and secondPathSS.goal.position ~= nil
                    and distance(firstPathSS.goal.position,portalSS.source) <= 1
                    and distance(secondPathSS.goal.position,portalSS.source) <= 1 then
                    local length = math.max(routeLength(firstPathSS,firstNativeSS.position,portalSS.source),
                        routeLength(secondPathSS,secondNativeSS.position,portalSS.source))
                    if selectedLength == nil or length < selectedLength then
                        selected,selectedLength = portalSS,length
                    end
                    rejection = "eligible; longest source route=" .. string.format("%.3f",length)
                else
                    rejection = "source path guard; first{" .. portalPathText(firstPathSS,portalSS.source)
                        .. "}; second{" .. portalPathText(secondPathSS,portalSS.source) .. "}"
                end
            else
                rejection = "destination offers no observed shared-hostile distance improvement"
            end
        end
        check(scenario.Log("Co-op portal candidate object=" .. portalSS.object_id .. " route=" .. portalSS.route_index
            .. " source_marker=" .. portalSS.marker_id .. " destination_marker=" .. portalSS.destination_marker_id
            .. " source=" .. positionText(portalSS.source) .. " destination=" .. positionText(portalSS.destination)
            .. " decision=" .. tostring(rejection)))
    end
    return selected
end

local function traversePortal(clients, baselines, portal)
    local routeKind = portal.route_index >= 0 and "recovered" or "authored"
    check(scenario.Log("Co-op native security portal route=" .. portal.route_index .. " object=" .. portal.object_id
        .. " source=" .. positionText(portal.source) .. " destination=" .. positionText(portal.destination)
        .. "; kind=" .. routeKind .. " source_marker=" .. portal.marker_id .. " destination_marker=" .. portal.destination_marker_id))
    captureBoth(clients,"coop-portal-before-" .. portal.object_id)
    local arrivals = {false,false}
    local deadline = scenario.Time()+180000
    local sourceWaits = {0,0}
    while scenario.Time() < deadline do
        for participant,client in ipairs(clients) do
            local worldSS,nativeSS = sample(client,baselines[participant])
            local currentPortalSS = matchingPortal(worldSS,portal)
            if currentPortalSS == nil then error("owned security portal identity changed during approach") end
            if distance(nativeSS.position,portal.destination) <= 6 and distance(worldSS.hero.position,portal.destination) <= 6 then
                if not arrivals[participant] then
                    check(scenario.Log("Co-op portal destination observed client=" .. client.ID
                        .. " native=" .. positionText(nativeSS.position) .. " server=" .. positionText(worldSS.hero.position)))
                end
                arrivals[participant] = true
            elseif arrivals[participant] then
                error("hero left the verified portal destination before its teammate arrived")
            else
                if not currentPortalSS.is_activation_available or not currentPortalSS.is_active then
                    error("security portal became inactive before native contact; no forced transfer")
                end
                if distance(nativeSS.position,portal.source) <= 0.35 then
                    sourceWaits[participant] = sourceWaits[participant]+1
                    if sourceWaits[participant] > 10 then error("portal source reached without an observed native/server transfer") end
                    check(scenario.Sleep(500)) -- Observe automatic contact; do not resend a completed click.
                else
                    approachRoute(clients,baselines,participant,nil,true,0.35,portal.source)
                end
            end
        end
        if arrivals[1] and arrivals[2] then
            visitedPortalIDs[portal.object_id] = true
            captureBoth(clients,"coop-portal-after-" .. portal.object_id)
            return
        end
    end
    error("both heroes did not traverse the observed active security portal within three minutes")
end

local function chooseTarget(clients, baselines, excludeds)
    for transition=0,6 do
        local objectID = chooseReachableTarget(clients,baselines,excludeds)
        if objectID ~= nil then return objectID end
        local portal = choosePortal(clients,baselines,excludeds)
        if transition == 6 or portal == nil then
            stop(clients,"no shared living ambient enemy has a complete route and no unvisited active forward security portal is reachable by both heroes")
        end
        traversePortal(clients,baselines,portal)
    end
    error("co-op route transition bound exhausted")
end

local function defeatTogether(clients, baselines, objectID, firstParticipant)
    approachTogether(clients,baselines,objectID)
    local deadline = scenario.Time()+60000
    local turn = firstParticipant or 1
    local missedWindows = 0
    while scenario.Time() < deadline do
        local firstSS = sample(clients[1], baselines[1])
        local secondSS = sample(clients[2], baselines[2])
        local firstTargetSS = targetByID(firstSS, objectID)
        local secondTargetSS = targetByID(secondSS, objectID)
        if firstTargetSS == nil or secondTargetSS == nil then
            stop(clients, "shared combat target left a bounded snapshot; death is unproven")
        end
        if firstTargetSS.is_dead and firstTargetSS.hit_point <= 0
            and secondTargetSS.is_dead and secondTargetSS.hit_point <= 0 then
            check(scenario.Log("Shared ambient target death confirmed object=" .. objectID))
            return
        end
        approach(clients, baselines, turn, objectID)
        if attackOnce(clients[turn], baselines[turn], objectID) then
            missedWindows = 0
        else
            missedWindows = missedWindows+1
            if missedWindows >= 6 then error("six completed co-op attack windows produced no confirmed damage") end
        end
        turn = 3-turn
    end
    stop(clients, "co-op target did not die within the bounded combat interval")
end

local function playMission(clients)
    inviteParty(clients)
    for _, client in ipairs(clients) do
        must(client.World:Configure({run_seed=104729, is_ambient_population_suppressed=false}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(clients[1].Mission:Select(1))
    must(clients[1].Mission:Start())
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Continue()); return nil end,
        second=function() must(clients[2].Mission:Continue()); return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Wait()); return nil end,
        second=function() must(clients[2].Mission:Wait()); return nil end,
    }))
    local baselines = {must(clients[1].World:Ready()), must(clients[2].World:Ready())}
    if baselines[1].game_id == 0 or baselines[1].game_id ~= baselines[2].game_id
        or baselines[1].zone_generation ~= baselines[2].zone_generation then
        stop(clients, "co-op clients did not enter the same committed mission epoch")
    end
    captureBoth(clients, "coop-entry")
    local excludeds = {}
    -- Each first-slot hero casts its observed native ability against its own live target.
    for participant=1,2 do
        local isCastComplete = false
        for encounter=1,6 do
            local objectID = chooseTarget(clients, baselines, excludeds)
            -- Stage outside basic-attack reach. The native cast still owns its
            -- real range/admission and any required ability approach.
            approachTogether(clients, baselines, objectID,12)
            isCastComplete = castSupported(clients[participant], baselines[participant], objectID)
            defeatTogether(clients, baselines, objectID)
            excludeds[objectID] = true
            if isCastComplete then break end
            check(scenario.Log("Ambient target died during native approach; shared death confirmed, choose another live ability target"))
        end
        if not isCastComplete then error("six encounters ended before the intended native ability could start") end
        captureBoth(clients, "coop-active-" .. participant)
    end
    -- Both other squad members must participate in ordinary basic combat.
    for slot=1,2 do
        check(scenario.Sleep(5200)) -- Swap cooldown allowance; SelectHero checks actual native/server eligibility.
        for index, client in ipairs(clients) do
            must(client.World:SelectHero(slot))
            local worldSS = sample(client, baselines[index])
            if worldSS.hero.index ~= slot then stop(clients, "co-op switch did not select the intended squad member") end
        end
        for participant=1,2 do
            local objectID = chooseTarget(clients, baselines, excludeds)
            defeatTogether(clients, baselines, objectID, participant)
            excludeds[objectID] = true
        end
        captureBoth(clients, "coop-squad-" .. slot)
    end
    local clearanceCount = 0
    for objectID in pairs(clearedObstacleIDs) do clearanceCount = clearanceCount+1 end
    local killCount = 0
    for objectID in pairs(excludeds) do killCount = killCount+1 end
    check(scenario.Log("Co-op native party, shared mission, supported abilities and all squad slots observed; shared ambient deaths="
        .. killCount .. "; separately cleared fixtures=" .. clearanceCount .. "; not full-mission or render parity proof"))
    return nil
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profiles = {
        must(server:CreateProfile({name="coop-first", hero_nouns={1667741389,3618328019,749013658}, chain_progression=1})),
        must(server:CreateProfile({name="coop-second", hero_nouns={1667741389,3618328019,749013658}, chain_progression=1})),
    }
    local clients = {
        must(profiles[1]:Launch({client_id="coop-first", launch_mode="normal"})),
        must(profiles[2]:Launch({client_id="coop-second", launch_mode="detached"})),
    }
    for _, client in ipairs(clients) do
        must(client:Login())
        must(client.Scene:Wait(scene.Arsenal))
    end
    local isComplete, missionError = pcall(playMission,clients)
    if not isComplete then
        captureBoth(clients,"coop-failed")
        error("co-op mission failed: " .. tostring(missionError))
    end
    return nil
end
