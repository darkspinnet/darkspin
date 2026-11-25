---Terminal chat commands: real native chat input, isolated disposable accounts.
---Write-only scenario work: do not infer native rendering from server replies.
local must = scenario.Must
local check = scenario.Check

local function capture(client, label)
    local report = must(client.Diagnostics:Capture(label))
    local image = must(client.UI:Screenshot(label))
    check(scenario.Log(label .. " manifest=" .. report.manifest_path .. " image=" .. image.image_path))
end

local function waitFor(label, timeout, observe)
    local deadline = scenario.Time() + timeout
    repeat
        local result = observe()
        if result then return result end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error(label .. " timed out; no command/input replay")
end

local function sameWorld(initial, current)
    if current.game_id ~= initial.game_id or current.zone_generation ~= initial.zone_generation
        or current.peer_generation ~= initial.peer_generation
        or current.transport_generation ~= initial.transport_generation then
        error("owned world identity changed during command observation")
    end
end

local function member(world, userID)
    if world.campaign then
        for _, participant in ipairs(world.campaign.members) do
            if participant.user_id == userID then return participant end
        end
    end
    error("owned campaign squad observation unavailable")
end

local function character(world, userID, index)
    for _, hero in ipairs(member(world, userID).characters) do
        if hero.index == index then return hero end
    end
    error("required squad slot absent from observation")
end

local function distanceSquared(first, second)
    return (first.x - second.x)^2 + (first.y - second.y)^2 + (first.z - second.z)^2
end

local function send(client, body, isExit)
    local cursor = must(client.Chat:Snapshot()).cursor
    if cursor == nil then error("fresh command cursor unavailable") end
    local opened = must(client.UI:Press("Enter"))
    if not opened.is_processed then error("chat opening unprocessed; no replay") end
    check(scenario.Sleep(250))
    local typed = must(client.UI:Type(body))
    if typed.processed_character_count ~= #body then error("partial command text; no replay") end
    must(client.UI:Screenshot("terminal-command-typed"))
    -- /exit posts WM_CLOSE locally: the owned process may close before this
    -- final key receipt or the server reply returns. Observe the process next.
    local submitted, submitError = client.UI:Press("Enter")
    if isExit then
        if submitError ~= nil then
            check(scenario.Log("/exit submit outcome unknown: " .. tostring(submitError)))
        elseif not submitted.is_processed then
            check(scenario.Log("/exit submit receipt unprocessed; checking exact owned process"))
        end
        return nil
    end
    submitted = must(submitted, submitError)
    if not submitted.is_processed then error("chat submission unprocessed; no replay") end
    local command = string.match(body, "^(/%S+)")
    local reply = must(client.Chat:Wait(cursor, command, 15000))
    local owner = must(client:Snapshot())
    if reply.command ~= command or reply.body ~= body or reply.source ~= "server_command_reply"
        or reply.is_native_rendered ~= false or reply.user_id ~= owner.user_id or reply.session_id <= 0 then
        error("command reply provenance or correlation mismatch")
    end
    check(scenario.Log(body .. " reply=" .. reply.reply .. " sequence=" .. tostring(reply.sequence)))
    return reply
end

local function accepted(client, body, expectedReply)
    local reply = send(client, body)
    if reply.reply ~= expectedReply then error(body .. " was not accepted: " .. reply.reply) end
    return reply
end

local function usable(client)
    local world = must(client.World:SnapshotLiving())
    local native = must(client.World:SnapshotNative()).gameplay_state
    if not native or not native.is_available or not native.is_world_active
        or not native.is_rig_observed or not native.is_rig_ready or not native.position
        or not native.controlled_handle or native.controlled_handle == 0
        or world.hero.input_lock_remaining_ms ~= 0 then return nil end
    return {world = world, native = native}
end

local function exerciseHero(client)
    local before = waitFor("usable native hero", 20000, function() return usable(client) end)
    local destination = nil
    -- Read-only mesh and native projection choose one finite movement input.
    for _, offset in ipairs({{4, 0}, {-4, 0}, {0, 4}, {0, -4}}) do
        local origin = before.native.position
        local goal = {x = origin.x + offset[1], y = origin.y + offset[2], z = origin.z}
        local path = must(client.World:Path(origin, goal))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal
            and distanceSquared(origin, path.goal.position) > 4 then
            local projected = must(client.World:Project(path.goal.position)).projection
            if projected and projected.is_available and projected.is_on_screen then
                destination = path.goal.position
                break
            end
        end
    end
    if not destination then error("no observed on-screen movement fixture route") end
    local click = must(client.World:Click(destination)).gameplay_click
    if not click or not click.is_press_poll_observed or not click.is_release_poll_observed then
        error("native movement input processing unknown; no replay")
    end
    if not click.is_native_ground_query_success or click.retained_handle ~= 0 then
        error("movement fixture selected an object instead of native ground; no replay")
    end
    waitFor("native and server movement after command", 10000, function()
        local current = usable(client)
        if not current then return nil end
        sameWorld(before.world, current.world)
        return current.world.hero.is_movement_available
            and distanceSquared(before.native.position, current.native.position) > 1
            and distanceSquared(before.world.hero.position, current.world.hero.movement_position) > 1
    end)
    local target = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local initial = must(target:Snapshot())
    if initial.is_dead or initial.hit_point <= 0 then error("post-command attack fixture not living") end
    local attack = must(client.World:Hold(initial.position, 1200)).gameplay_click
    if not attack or not attack.is_press_poll_observed or not attack.is_release_poll_observed then
        error("native attack input processing unknown; no replay")
    end
    waitFor("actual post-command hero attack damage", 12000, function()
        local current = must(target:Snapshot())
        return current.is_available and (current.is_dead or current.hit_point < initial.hit_point)
    end)
end

local function kill(client)
    must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    local before = must(client.World:SnapshotLiving())
    local enemies = {}
    for index = 1, 5 do
        local enemy = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
        local initial = must(enemy:Snapshot())
        if initial.is_dead or initial.hit_point <= 0 then error("kill fixture not living") end
        must(enemy:SetAI(true))
        enemies[index] = enemy
    end
    -- Damage from enabled AI proves busy combat, rather than queued spawn receipts.
    waitFor("busy autonomous combat before /kill", 20000, function()
        local world = must(client.World:SnapshotLiving())
        sameWorld(before, world)
        return world.hero.hit_point < before.hero.hit_point
    end)
    for _, enemy in ipairs(enemies) do
        local current = must(enemy:Snapshot())
        if not current.is_available or not current.is_ai_enabled or current.is_dead or current.hit_point <= 0 then
            error("tracked enemy was not alive and active immediately before /kill")
        end
    end
    capture(client, "terminal-kill-busy-before")
    accepted(client, "/kill", "Defeating all live enemies")
    waitFor("all tracked live enemies actually defeated", 15000, function()
        sameWorld(before, must(client.World:SnapshotLiving()))
        for _, enemy in ipairs(enemies) do
            local current = must(enemy:Snapshot())
            if not current.is_available or not current.is_dead or current.hit_point > 0 then return nil end
        end
        return true
    end)
end

local function victory(client)
    local before = must(client.World:SnapshotLiving())
    if before.is_zone_terminal or before.is_victory_committed or (before.boss and before.boss.is_complete) then
        error("victory fixture already complete")
    end
    accepted(client, "/victory", "Completing the active level with victory")
    waitFor("actual nonzero boss completion", 30000, function()
        local world = must(client.World:Snapshot())
        sameWorld(before, world)
        return world.boss and world.boss.leader_object_id ~= 0 and world.boss.is_complete
    end)
    capture(client, "terminal-victory-boss-complete")
    local beam = must(client.Mission:BeamOut())
    if not beam.world.is_victory_available or not beam.world.is_victory_committed then
        error("personal victory transaction did not commit")
    end
    waitFor("actual native ship results after victory", 120000, function()
        local deployment = must(client.Mission:SnapshotDeployment()).deployment_state
        return deployment and deployment.is_available and deployment.is_loaded and deployment.is_results_screen
    end)
end

local function defeat(client, profile)
    local before = must(client.World:SnapshotLiving())
    for index = 0, 2 do
        local hero = character(before, profile.user_id, index)
        if hero.hit_point <= 0 or hero.is_defeated then error("defeat fixture must start with three living heroes") end
    end
    accepted(client, "/defeat", "Completing the campaign with defeat")
    waitFor("real terminal squad Game Over", 15000, function()
        local world = must(client.World:Snapshot())
        sameWorld(before, world)
        if not world.is_zone_terminal or world.is_victory_committed then return nil end
        for index = 0, 2 do
            local hero = character(world, profile.user_id, index)
            if not hero.is_defeated or hero.hit_point > 0 then return nil end
        end
        return true
    end)
    -- The API has no native Game Over screen producer. Server terminal plus all
    -- dead squad slots is the asserted outcome; the screenshot is review evidence.
    check(scenario.Log("/defeat: server Game Over observed; native Game Over presentation requires capture review"))
end

local function reset(client)
    local before = must(client.World:SnapshotLiving())
    send(client, "/damage " .. tostring(before.hero.hit_point / 2))
    waitFor("damaged reset fixture", 15000, function()
        local current = must(client.World:SnapshotLiving())
        sameWorld(before, current)
        return current.hero.hit_point > 0 and current.hero.hit_point < before.hero.hit_point
    end)
    capture(client, "terminal-reset-before")
    -- Fang may process native WM_RESET before server authorization. Recovery is
    -- independently checked after the correlated accepted administrative reply.
    local damaged = must(client.World:SnapshotLiving())
    accepted(client, "/reset", "Resetting transient gameplay state")
    local recovered = waitFor("reset native recovery", 20000, function() return usable(client) end)
    sameWorld(before, recovered.world)
    if recovered.world.hero.object_id ~= damaged.hero.object_id
        or recovered.world.hero.hit_point ~= damaged.hero.hit_point then
        error("reset replaced the hero or changed retained health")
    end
    exerciseHero(client)
end

local function recap(client, profile)
    local initial = must(client.World:SnapshotLiving())
    for index = 0, 1 do
        local active = must(client.World:SnapshotLiving())
        if active.hero.index ~= index then must(client.World:SelectHero(index, 30000)) end
        active = must(client.World:SnapshotLiving())
        local fallenIndex = index
        send(client, "/damage " .. tostring(active.hero.hit_point + 1))
        waitFor("real fallen squad slot " .. tostring(index), 15000, function()
            local world = must(client.World:Snapshot())
            sameWorld(initial, world)
            local fallen = character(world, profile.user_id, fallenIndex)
            return fallen.is_defeated and fallen.hit_point <= 0
        end)
        local current = must(client.World:Snapshot())
        if not current.hero or current.hero.index ~= index + 1 or current.hero.is_defeated then
            must(client.World:SelectHero(index + 1, 30000))
        end
        waitFor("living reserve after real death", 30000, function() return usable(client) end)
    end
    capture(client, "terminal-recap-two-fallen")
    accepted(client, "/recap", "Resurrecting fallen heroes across the party")
    waitFor("both fallen squad members resurrected", 15000, function()
        local world = must(client.World:SnapshotLiving())
        sameWorld(initial, world)
        for index = 0, 2 do
            local hero = character(world, profile.user_id, index)
            if hero.hit_point <= 0 or hero.is_defeated then return nil end
        end
        return true
    end)
    must(client.World:SelectHero(0, 30000))
    exerciseHero(client)
end

local function denied(client)
    local enemy = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local beforeNPC = must(enemy:Snapshot())
    local before = must(client.World:SnapshotLiving())
    if beforeNPC.hit_point <= 0 or beforeNPC.is_dead or beforeNPC.is_ai_enabled then
        error("permission fixture must be living and idle")
    end
    for _, command in ipairs({"/kill", "/victory"}) do
        local reply = send(client, command)
        if reply.reply ~= "This command requires administrator access on this server." then
            error("ordinary-role command did not return explicit authorization denial")
        end
        local deadline = scenario.Time() + 2500
        repeat
            local world = must(client.World:SnapshotLiving())
            local currentNPC = must(enemy:Snapshot())
            sameWorld(before, world)
            if currentNPC.is_dead or currentNPC.hit_point ~= beforeNPC.hit_point
                or world.is_zone_terminal or world.is_victory_committed or world.is_objective_complete
                or (world.boss and world.boss.is_complete)
                or (world.boss and world.boss.leader_object_id or 0)
                    ~= (before.boss and before.boss.leader_object_id or 0) then
                error("denied " .. command .. " changed NPC or campaign outcome")
            end
            check(scenario.Sleep(100))
        until scenario.Time() >= deadline
        capture(client, "terminal-denied-" .. string.sub(command, 2))
    end
end

local function exit(client)
    local before = must(client:Snapshot())
    if not before.is_running or before.pid ~= client.PID or before.instance_id ~= client.InstanceID then
        error("exit fixture does not own a live exact client process")
    end
    capture(client, "terminal-exit-before-close")
    send(client, "/exit", true)
    waitFor("exact owned client process exit", 15000, function()
        local current = must(client:Snapshot())
        if current.pid ~= before.pid or current.instance_id ~= before.instance_id then
            error("client identity changed while observing /exit")
        end
        return not current.is_running
    end)
    check(scenario.Log("/exit stopped owned pid=" .. tostring(before.pid) .. " instance=" .. before.instance_id))
end

local function runFlow(server, name, isAdmin, flow)
    local profile = must(server:CreateProfile({name = "terminal-" .. name,
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1, is_admin = isAdmin}))
    local client = must(profile:Launch({client_id = "terminal-" .. name, launch_mode = "normal",
        screen_width = 1280, screen_height = 720, native_ui_scale_percent = 100,
        is_intro_skipped = true, is_cinematic_skipped = true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    waitFor("initial native hero", 30000, function() return usable(client) end)
    local isSuccessful, failure = pcall(flow, client, profile)
    if not isSuccessful then
        -- Keep the live owned state for diagnosis if capture itself fails.
        capture(client, "terminal-" .. name .. "-failed")
        error(tostring(failure))
    end
    if name ~= "exit" then
        capture(client, "terminal-" .. name .. "-passed")
        check(client:Terminate())
    end
    check(profile:Logout())
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    runFlow(server, "denied", false, denied)
    runFlow(server, "kill", true, kill)
    runFlow(server, "victory", true, victory)
    runFlow(server, "defeat", true, defeat)
    runFlow(server, "reset", true, reset)
    runFlow(server, "recap", true, recap)
    runFlow(server, "exit", true, exit)
end
