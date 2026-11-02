---Build-103 pending named-boss actions through authored campaign triggers, one server.
local must, check = scenario.Must, scenario.Check
-- Completed action captures and health-gated gaps are tracked in
-- bin/game/logs/npc-boss-20261028/findings.md. Keep only actionable gaps here.
local jobs = {}

local function pause(ms) check(scenario.Sleep(ms)) end
local function record(stage, label, fields)
    local parts = {"npc_boss_sweep", label, "stage=" .. stage}
    for name, field in pairs(fields) do
        parts[#parts + 1] = name .. "=" .. tostring(field)
    end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end

local function launch(server, stage, role)
    local name = "boss-" .. stage .. "-" .. role
    local profile = must(server:CreateProfile({name = name,
        hero_nouns = {1667741389, 117394854, 576185321}, chain_progression = stage - 1}))
    local mode = stage == 4 and role == "victim" and "normal" or "detached"
    local client = must(profile:Launch({client_id = name, launch_mode = mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    record(stage, "client", {role = role, pid = client.PID})
    return client, profile
end

local function memberHP(world, userID)
    if not world.campaign then return nil end
    for _, member in ipairs(world.campaign.members) do
        if member.user_id == userID then
            local actor = member.characters[member.deployed_index + 1]
            return actor and actor.hit_point or nil
        end
    end
    return nil
end

local function sample(stage, victim, observer, victimID, observerID, label)
    local world = must(victim.World:SnapshotLiving())
    local peer = must(observer.World:SnapshotLiving())
    record(stage, label, {boss = world.boss and world.boss.leader_object_id,
        phase = world.boss and world.boss.phase,
        victim_hp = world.hero.hit_point, observer_hp = peer.hero.hit_point,
        victim_remote_hp = memberHP(peer, victimID),
        observer_remote_hp = memberHP(world, observerID),
        victim_x = world.hero.position.x, victim_y = world.hero.position.y,
        observer_x = peer.hero.position.x, observer_y = peer.hero.position.y})
    return world
end

local function castPosition(victim, bossPosition, distance)
    local offsets = {{distance, 0}, {-distance, 0}, {0, distance}, {0, -distance}}
    for _, offset in ipairs(offsets) do
        local requested = {x = bossPosition.x + offset[1],
            y = bossPosition.y + offset[2], z = bossPosition.z}
        local path = victim.World:Path(bossPosition, requested)
        if path and path.is_available and path.is_complete and path.goal
            and path.goal.distance <= 0.75 then
            return path.goal.position
        end
    end
    return nil
end

local function prepareStage(server, job)
    local stage = job.stage
    local victim, victimProfile = launch(server, stage, "victim")
    local observer, observerProfile = launch(server, stage, "observer")
    must(victim.Party:Create("boss-sweep-" .. stage))
    must(victim.Party:Invite(observer))
    must(observer.Party:Join(victim))
    for _, client in ipairs({victim, observer}) do
        must(client.World:Configure({run_seed = 104729,
            is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(victim.Mission:Select(stage))
    must(victim.Mission:Start())
    must(observer.Mission:WaitDeployment())
    check(scenario.Parallel({
        victim = function() must(victim.Mission:Continue()); return nil end,
        observer = function() must(observer.Mission:Continue()); return nil end,
    }))
    must(victim.Mission:Wait())
    must(observer.Mission:Wait())
    local world = must(victim.World:Ready())
    must(observer.World:Ready())
    job.victim, job.observer = victim, observer
    job.victimProfile, job.observerProfile = victimProfile, observerProfile
    job.world = world
end

local function runStage(job)
    local stage = job.stage
    local victim, observer = job.victim, job.observer
    local victimProfile, observerProfile = job.victimProfile, job.observerProfile
    local world = job.world
    record(stage, "world", {game = world.game_id,
        trigger_count = #world.boss_triggers, boss_phase = world.boss and world.boss.phase})
    if #world.boss_triggers == 0 then
        job.status = "missing_trigger"
    else
        local trigger = world.boss_triggers[1]
        record(stage, "trigger", {marker = trigger.marker_id, set = trigger.marker_set_name,
            x = trigger.position.x, y = trigger.position.y, z = trigger.position.z,
            radius = trigger.radius})
        must(victim.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
        must(observer.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
        must(victim.World:PlaceHero(trigger.position))
        must(observer.World:PlaceHero(trigger.position))
        local deadline = scenario.Time() + 45000
        local bossPosition = nil
        while scenario.Time() < deadline do
            world = must(victim.World:SnapshotLiving())
            if world.boss and world.boss.leader_object_id ~= 0 then
                for _, npc in ipairs(world.npcs) do
                    if npc.object_id == world.boss.leader_object_id then
                        bossPosition = npc.position
                        record(stage, "boss_actor", {noun = npc.noun,
                            object = npc.object_id, x = npc.position.x,
                            y = npc.position.y, z = npc.position.z})
                        break
                    end
                end
                break
            end
            pause(500)
        end
        if not bossPosition then
            job.status = "boss_not_admitted"
        else
            local goal = castPosition(victim, bossPosition, job.distance)
            if goal then
                must(victim.World:PlaceHero(goal))
                must(observer.World:PlaceHero(goal))
                record(stage, "cast_position", {x = goal.x, y = goal.y, z = goal.z})
            else
                record(stage, "cast_position", {status = "no_complete_mesh_path"})
            end
            sample(stage, victim, observer, victimProfile.user_id, observerProfile.user_id, "capture_start")
            for interval = 1, job.capture_count or 6 do
                pause(5000)
                if job.hold_position and goal then
                    must(victim.World:PlaceHero(goal))
                    must(observer.World:PlaceHero(goal))
                end
                sample(stage, victim, observer, victimProfile.user_id, observerProfile.user_id,
                    "capture_" .. interval)
            end
            job.status = "active"
        end
    end
    record(stage, "case_end", {status = job.status, ability = job.ability})
end

return function()
    check(scenario.RequireAPI(1))
    if #jobs == 0 then
        record(0, "suite_end", {stages = 0, status = "no_pending_position_cases"})
        return
    end
    local server = must(scenario.StartServer({}))
    for startIndex = 1, #jobs, 4 do
        local branches = {}
        local lastIndex = math.min(startIndex + 3, #jobs)
        for index = startIndex, lastIndex do
            local job = jobs[index]
            prepareStage(server, job)
            branches["stage_" .. job.stage] = function() runStage(job) end
        end
        check(scenario.Parallel(branches))
        for index = startIndex, lastIndex do
            local job = jobs[index]
            check(job.victim:Terminate())
            check(job.observer:Terminate())
            check(job.victimProfile:Logout())
            check(job.observerProfile:Logout())
        end
    end
    for _, job in ipairs(jobs) do
        if job.status == nil then error("boss stage did not finish: " .. job.stage) end
    end
    record(0, "suite_end", {stages = #jobs})
end
