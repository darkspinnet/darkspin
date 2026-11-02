---Two owned clients fight three installed nouns in a shared focused 1-1 scene.
---Separate normal/detached rounds prove each controller before a joint round.
---Captures preserve independent snapshots; publication is not native visibility.
local must = scenario.Must
local check = scenario.Check

---@class ZooParticipant
---@field client ManagedClient
---@field baseline WorldSnapshot|nil
---@field nativeHandle integer|nil
---@field nativeThread integer|nil
---@field nativeFrame integer|nil

---@class ZooCase
---@field label string
---@field noun string
---@field mode 'normal'|'detached'|'both'
---@field spawnIndex integer

---@param worldSS WorldSnapshot
---@param objectID integer
---@return WorldNPC|nil
local function findNPC(worldSS, objectID)
    for _, npc in ipairs(worldSS.npcs) do
        if npc.object_id == objectID then
            return npc
        end
    end
    return nil
end

---@param participant ZooParticipant
---@return WorldSnapshot|nil,string|nil
local function observeWorld(participant)
    local worldSS, observationError = participant.client.World:Snapshot()
    if observationError ~= nil or worldSS == nil then
        return nil, "world observation: " .. tostring(observationError)
    end
    if not worldSS.is_available or worldSS.hero == nil or worldSS.hero.is_defeated
        or worldSS.hero.hit_point <= 0 or worldSS.hero.index ~= 0 then
        return nil, "world unavailable or active hero defeated/changed"
    end
    local baseline = participant.baseline
    if baseline ~= nil and (worldSS.instance_id ~= baseline.instance_id or worldSS.pid ~= baseline.pid
        or worldSS.user_id ~= baseline.user_id or worldSS.game_id ~= baseline.game_id
        or worldSS.peer_generation ~= baseline.peer_generation
        or worldSS.transport_generation ~= baseline.transport_generation
        or worldSS.zone_generation ~= baseline.zone_generation
        or baseline.hero == nil or worldSS.hero.object_id ~= baseline.hero.object_id) then
        return nil, "owned process/account/game/peer/zone/actor changed"
    end
    return worldSS, nil
end

---@param participant ZooParticipant
---@return string|nil
local function observeNative(participant)
    local baseline = participant.baseline
    if baseline == nil then
        return "native observation requires the pinned world"
    end
    local deadline = scenario.Time() + 5000
    local lastError = "no advancing gameplay frame"
    while scenario.Time() < deadline do
        local receipt, observationError = participant.client.World:SnapshotNative()
        local native = receipt ~= nil and receipt.gameplay_state or nil
        if observationError == nil and receipt ~= nil and receipt.is_processed and native ~= nil
            and native.is_available and native.is_world_active and native.controlled_handle ~= nil
            and native.controlled_handle ~= 0 and native.position ~= nil then
            if receipt.instance_id ~= baseline.instance_id or receipt.pid ~= baseline.pid then
                return "native process changed"
            end
            if participant.nativeHandle ~= nil and (native.controlled_handle ~= participant.nativeHandle
                or native.owner_thread_id ~= participant.nativeThread) then
                return "native controlled actor or owner thread changed"
            end
            if participant.nativeFrame == nil or native.frame_sequence > participant.nativeFrame then
                participant.nativeHandle = native.controlled_handle
                participant.nativeThread = native.owner_thread_id
                participant.nativeFrame = native.frame_sequence
                return nil
            end
        elseif observationError ~= nil then
            lastError = tostring(observationError)
        end
        local sleepError = scenario.Sleep(100)
        if sleepError ~= nil then
            return "native observation wait: " .. tostring(sleepError)
        end
    end
    return "native observation deadline: " .. lastError
end

---@param participants ZooParticipant[]
---@param label string
---@return string|nil
local function captureBoth(participants, label)
    local failures = {}
    for index, participant in ipairs(participants) do
        local capture, captureError = participant.client.Diagnostics:Capture(label)
        if captureError ~= nil or capture == nil or not capture.is_complete then
            failures[#failures + 1] = tostring(index) .. ":" .. tostring(captureError)
        end
    end
    if #failures > 0 then
        return "capture " .. label .. ": " .. table.concat(failures, "; ")
    end
    return nil
end

---@param participants ZooParticipant[]
---@param message string
---@return string
local function fail(participants, message)
    local captureError = captureBoth(participants, "zoo-failed")
    return message .. (captureError ~= nil and "; " .. captureError or "; saved zoo-failed captures")
end

---@param participant ZooParticipant
---@param point WorldPosition
---@return string|nil
local function holdTarget(participant, point)
    local receipt, actionError = participant.client.World:Hold(point, 1000)
    if actionError ~= nil or receipt == nil then
        return "hold stopped without replay: " .. tostring(actionError)
    end
    local baseline = participant.baseline
    local click, native = receipt.gameplay_click, receipt.gameplay_state
    if baseline == nil or receipt.pid ~= baseline.pid or receipt.instance_id ~= baseline.instance_id
        or not receipt.is_processed or click == nil or not click.is_projected
        or not click.is_press_poll_observed or not click.is_release_poll_observed
        or native == nil or not native.is_available or native.controlled_handle ~= participant.nativeHandle
        or native.owner_thread_id ~= participant.nativeThread then
        return "hold omitted pinned owner/actor or paired processing evidence; no replay"
    end
    return nil
end

---@param participants ZooParticipant[]
---@param mode 'normal'|'detached'|'both'
---@param point WorldPosition
---@return string|nil
local function attackRound(participants, mode, point)
    if mode == "normal" then
        return holdTarget(participants[1], point)
    end
    if mode == "detached" then
        return holdTarget(participants[2], point)
    end
    local parallelError = scenario.Parallel({
        normal = function()
            return holdTarget(participants[1], point)
        end,
        detached = function()
            return holdTarget(participants[2], point)
        end,
    })
    if parallelError ~= nil then
        return "joint holds stopped without replay: " .. tostring(parallelError)
    end
    return nil
end

---@param participants ZooParticipant[]
---@return WorldSnapshot[]|nil,string|nil
local function observeBoth(participants)
    local worlds = {}
    for index, participant in ipairs(participants) do
        local worldSS, observationError = observeWorld(participant)
        if worldSS == nil then
            return nil, tostring(index) .. ":" .. tostring(observationError)
        end
        worlds[index] = worldSS
    end
    if worlds[1].game_id ~= worlds[2].game_id or worlds[1].zone_generation ~= worlds[2].zone_generation then
        return nil, "participants no longer share the same game/zone"
    end
    return worlds, nil
end

---@param participants ZooParticipant[]
---@param case ZooCase
---@return string|nil
local function runCase(participants, case)
    local spawn, spawnError = participants[case.spawnIndex].client.World:Spawn(case.noun)
    if spawnError ~= nil or spawn == nil or not spawn.is_published or spawn.object_id == 0 then
        return fail(participants, case.label .. " spawn stopped without requeue: " .. tostring(spawnError))
    end
    local worlds, observationError = observeBoth(participants)
    if worlds == nil then
        return fail(participants, case.label .. ": " .. tostring(observationError))
    end
    local target = findNPC(worlds[1], spawn.object_id)
    local remoteTarget = findNPC(worlds[2], spawn.object_id)
    if target == nil or remoteTarget == nil or target.noun ~= case.noun or remoteTarget.noun ~= case.noun
        or target.is_dead or remoteTarget.is_dead or target.hit_point <= 0 or remoteTarget.hit_point <= 0 then
        return fail(participants, case.label .. " exact published noun/ID not observed alive from both participants")
    end
    local captureError = captureBoth(participants, case.label .. "-before")
    if captureError ~= nil then
        return fail(participants, captureError)
    end
    local previousHitPoint = target.hit_point
    local holdCount = 0
    local isNextHoldAllowed = true
    local deadline = scenario.Time() + 20000
    while scenario.Time() < deadline do
        if isNextHoldAllowed and holdCount < 8 then
            local actionError = attackRound(participants, case.mode, target.position)
            if actionError ~= nil then
                return fail(participants, case.label .. " object=" .. tostring(spawn.object_id) .. ": " .. actionError)
            end
            holdCount = holdCount + 1
            isNextHoldAllowed = false
        end
        worlds, observationError = observeBoth(participants)
        if worlds == nil then
            return fail(participants, case.label .. ": " .. tostring(observationError))
        end
        target = findNPC(worlds[1], spawn.object_id)
        remoteTarget = findNPC(worlds[2], spawn.object_id)
        if target == nil or remoteTarget == nil then
            return fail(participants, case.label .. " target disappeared; death is unproven")
        end
        local isNormalDead = target.is_dead and target.hit_point <= 0
        local isDetachedDead = remoteTarget.is_dead and remoteTarget.hit_point <= 0
        if isNormalDead and isDetachedDead then
            for _, participant in ipairs(participants) do
                local nativeError = observeNative(participant)
                if nativeError ~= nil then
                    return fail(participants, case.label .. ": " .. nativeError)
                end
            end
            return captureBoth(participants, case.label .. "-after")
        end
        if target.hit_point < previousHitPoint then
            previousHitPoint = target.hit_point
            -- Independent intended-target damage permits another normal input.
            -- A native receipt or an opaque picked handle alone never does.
            isNextHoldAllowed = not isNormalDead and not isDetachedDead
        end
        local sleepError = scenario.Sleep(150)
        if sleepError ~= nil then
            return fail(participants, case.label .. " observation wait: " .. tostring(sleepError))
        end
    end
    return fail(participants, case.label .. " object=" .. tostring(spawn.object_id)
        .. " noun=" .. case.noun .. " mode=" .. case.mode .. " holds=" .. tostring(holdCount)
        .. " remaining_hp=" .. tostring(target.hit_point) .. "; twenty-second progress bound exhausted")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local normalProfile = must(server:CreateProfile({
        name = "zoo-normal", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local detachedProfile = must(server:CreateProfile({
        name = "zoo-detached", hero_nouns = {576185321, 3618328019, 749013658}, chain_progression = 1,
    }))
    local normal = must(normalProfile:Launch({client_id = "normal", launch_mode = "normal"}))
    local detached = must(detachedProfile:Launch({client_id = "detached", launch_mode = "detached"}))
    local participants = {{client = normal}, {client = detached}} ---@type ZooParticipant[]
    must(normal:Login())
    must(detached:Login())
    must(normal.Scene:Wait(scene.Arsenal))
    must(detached.Scene:Wait(scene.Arsenal))
    must(normal.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(detached.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(normal.Party:Create("noun-zoo"))
    must(normal.Party:Invite(detached))
    must(detached.Party:Join(normal))
    must(normal.Scene:Set(scene.Navigation))
    must(normal.Mission:Select(1))
    must(normal.Mission:Start())
    must(detached.Mission:WaitDeployment())
    check(scenario.Parallel({
        normal = function()
            must(normal.Mission:Continue())
            return nil
        end,
        detached = function()
            must(detached.Mission:Continue())
            return nil
        end,
    }))
    must(normal.Mission:Wait())
    must(detached.Mission:Wait())
    local worlds, observationError = observeBoth(participants)
    if worlds == nil then
        return fail(participants, "shared mission: " .. tostring(observationError))
    end
    if worlds[1].user_id == worlds[2].user_id or worlds[1].pid == worlds[2].pid
        or worlds[1].hero == nil or worlds[2].hero == nil
        or worlds[1].hero.object_id == worlds[2].hero.object_id then
        return fail(participants, "shared mission did not retain distinct accounts, processes and deployed heroes")
    end
    for index, participant in ipairs(participants) do
        participant.baseline = worlds[index]
        local nativeError = observeNative(participant)
        if nativeError ~= nil then
            return fail(participants, "initial native actor: " .. nativeError)
        end
    end
    local captureError = captureBoth(participants, "zoo-shared")
    if captureError ~= nil then
        return fail(participants, captureError)
    end
    local cases = { ---@type ZooCase[]
        {label = "zoo-01-normal", noun = "CitadelBasicGunner.Noun", mode = "normal", spawnIndex = 1},
        {label = "zoo-02-detached", noun = "ZelemBasicPackMelee.Noun", mode = "detached", spawnIndex = 2},
        {label = "zoo-03-joint", noun = "ZelemSpecialThree.Noun", mode = "both", spawnIndex = 1},
    }
    for _, case in ipairs(cases) do
        local caseError = runCase(participants, case)
        if caseError ~= nil then
            return caseError
        end
    end
    -- Both clients remain available for authenticated endpoint inspection.
    return captureBoth(participants, "zoo-complete")
end
