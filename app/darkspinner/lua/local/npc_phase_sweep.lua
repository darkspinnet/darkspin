---Pending autonomous NPC phases on one fixed-port server.
local must, check = scenario.Must, scenario.Check
local jobs = {{id = "catalog", catalog_only = true}}
local function log(job, label, fields)
    local parts = {"npc_phase_sweep", label, "case=" .. job.id}
    for name, field in pairs(fields) do
        parts[#parts + 1] = name .. "=" .. tostring(field)
    end
    table.sort(parts)
    check(scenario.Log(table.concat(parts, " ")))
end
local function launch(server, job, role)
    local profile = must(server:CreateProfile({name = "phase-" .. job.id .. "-" .. role,
        hero_nouns = {role == "victim" and job.hero or 1667741389, 117394854, 576185321}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "phase-" .. job.id .. "-" .. role,
        launch_mode = "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    log(job, "client", {role = role, pid = client.PID, user = profile.user_id})
    return client, profile
end
local function prepare(server, job)
    job.victim, job.victimProfile = launch(server, job, "victim")
    job.observer, job.observerProfile = launch(server, job, "observer")
    must(job.victim.Party:Create("phase-" .. job.id))
    must(job.victim.Party:Invite(job.observer))
    must(job.observer.Party:Join(job.victim))
    for _, client in ipairs({job.victim, job.observer}) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(job.victim.Mission:Select(1))
    must(job.victim.Mission:Start())
    must(job.observer.Mission:WaitDeployment())
    check(scenario.Parallel({
        victim = function() must(job.victim.Mission:Continue()) end,
        observer = function() must(job.observer.Mission:Continue()) end,
    }))
    must(job.victim.Mission:Wait())
    must(job.observer.Mission:Wait())
    for _, client in ipairs({job.victim, job.observer}) do
        must(client.World:Ready())
        must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
    end
end
local function living(job, client)
    local deadline = scenario.Time() + 3000
    repeat
        local world = must(client.World:Snapshot())
        if world.is_available then
            if world.is_zone_terminal or not world.hero or world.hero.is_defeated or world.hero.hit_point <= 0 then
                error("available world lost living hero: " .. client.ID)
            end
            return world
        end
        log(job, "unavailable", {client = client.ID, reason = world.unavailable_reason})
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    error("world remained unavailable: " .. client.ID)
end
local function hitOwner(job, objectID)
    local world = living(job, job.observer)
    for _, npc in ipairs(world.npcs) do
        if npc.object_id == objectID then
            if npc.is_dead then error("healing owner died before native hit") end
            local aim = {x = npc.position.x, y = npc.position.y, z = npc.position.z + 1.5}
            local projection = must(job.observer.World:Project(aim)).projection
            if not projection or not projection.is_available or not projection.is_on_screen then
                error("healing owner is outside native projection")
            end
            local receipt = must(job.observer.World:Hold(aim, 150))
            local click = receipt.gameplay_click
            if not receipt.is_processed or not click or not click.is_press_poll_observed
                or not click.is_release_poll_observed then
                error("owner native hit input lacked paired polls; no replay")
            end
            log(job, "native_hit_input", {object = objectID, before_hp = npc.hit_point})
            return
        end
    end
    error("healing owner unavailable before native hit")
end
local function stageSource(job)
    local source = must(job.victim.World:SpawnControlledNPC(job.noun))
    local actor = must(source:Snapshot())
    local world = living(job, job.victim)
    for _, direction in ipairs({{1,0}, {-1,0}, {0,1}, {0,-1},
        {0.7071,0.7071}, {-0.7071,0.7071}, {0.7071,-0.7071}, {-0.7071,-0.7071}}) do
        local requested = {x = world.hero.position.x + direction[1] * job.distance,
            y = world.hero.position.y + direction[2] * job.distance, z = world.hero.position.z}
        local dx, dy = requested.x - actor.position.x, requested.y - actor.position.y
        if dx * dx + dy * dy <= 38 * 38 then
            local path = must(job.victim.World:Path(actor.position, requested))
            if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                if dx * dx + dy * dy > 19 * 19 then
                    local midpoint = {x = (actor.position.x + path.goal.position.x) / 2,
                        y = (actor.position.y + path.goal.position.y) / 2,
                        z = (actor.position.z + path.goal.position.z) / 2}
                    local midpointPath = must(job.victim.World:Path(actor.position, midpoint))
                    if not midpointPath.is_available or not midpointPath.is_complete or not midpointPath.goal then
                        error("staged source midpoint path unavailable")
                    end
                    local step = must(source:Move(midpointPath.goal.position))
                    if not step.is_completed then error("staged source midpoint movement incomplete") end
                end
                local moved = must(source:Move(path.goal.position))
                if not moved.is_completed then error("staged source did not complete movement") end
                actor = must(source:Snapshot())
                world = living(job, job.victim)
                dx, dy = actor.position.x - world.hero.position.x, actor.position.y - world.hero.position.y
                if dx * dx + dy * dy <= 10 * 10 then error("staged source is not outside melee selector") end
                must(source:SetAI(true))
                log(job, "staged_source", {object = source.ObjectID, distance = math.sqrt(dx * dx + dy * dy)})
                return source.InitialSnapshot
            end
        end
    end
    error("staged source lacks a complete bounded mesh path")
end
local function summonCompanion(job)
    local source = must(job.victim.World:SpawnControlledNPC(job.noun))
    local window = must(job.victim.UI:InspectWindow()).window
    if not window or window.width ~= 1280 or window.height ~= 720 then
        error("native summon requires the established 1280x720 hotbar layout")
    end
    local native = must(job.victim.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready or not native.position then
        error("native summon hero rig is unavailable")
    end
    local aim = {x = native.position.x, y = native.position.y, z = native.position.z + 1.5}
    local projection = must(job.victim.World:Project(aim)).projection
    if not projection or not projection.is_on_screen then error("native summon aim is off screen") end
    local button = must(job.victim.UI:Click(640, 655))
    if not button.is_processed then error("summon hotbar input unprocessed; no replay") end
    check(scenario.Sleep(300))
    local cast = must(job.victim.World:Click(aim))
    if not cast.is_processed then error("summon world input unprocessed; no replay") end
    log(job, "native_summon_input", {hero = job.hero, ability = "SummonBeast"})
    check(scenario.Sleep(3000))
    must(source:SetAI(true))
    return source.InitialSnapshot
end
local function moveOwner(job)
    local world = living(job, job.victim)
    for _, offset in ipairs({{15,0}, {-15,0}, {0,15}, {0,-15}}) do
        local goal = {x = world.hero.position.x + offset[1],
            y = world.hero.position.y + offset[2], z = world.hero.position.z}
        local path = must(job.victim.World:Path(world.hero.position, goal))
        if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
            local projection = must(job.victim.World:Project(path.goal.position)).projection
            if projection and projection.is_on_screen then
                local click = must(job.victim.World:Click(path.goal.position))
                if not click.is_processed then error("owner movement input unprocessed; no replay") end
                log(job, "owner_move_input", {x = path.goal.position.x, y = path.goal.position.y})
                return
            end
        end
    end
    error("owner movement lacks a visible complete mesh path")
end
local function run(job)
    if job.catalog_only then
        local catalog = must(job.victim.World:NPCCatalog())
        for _, entry in ipairs(catalog.entries) do
            log(job, "catalog_entry", {noun = entry.noun, ability = entry.ability_name,
                rank = entry.rank, family = entry.family, hp = entry.hit_point,
                is_recovered = entry.is_action_recovered,
                is_manual = entry.is_control_supported, is_staging = entry.is_staging_supported,
                is_boss = entry.is_final_boss,
                manual = table.concat(entry.supported_ability_names or {}, ","),
                autonomous = table.concat(entry.autonomous_ability_names or {}, ",")})
        end
        log(job, "catalog_end", {count = #catalog.entries})
        return
    end
    if job.helper then
        job.ally = must(job.victim.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
        log(job, "helper", {object = must(job.ally:Snapshot()).identity.object_id})
    end
    local receipt
    if job.companion_slow then
        receipt = summonCompanion(job)
    elseif job.staged_source then
        receipt = stageSource(job)
    else
        receipt = must(job.victim.World:Spawn(job.noun))
    end
    log(job, "spawn", {noun = receipt.noun, object = receipt.object_id,
        game = receipt.game_id, is_ai_enabled = job.staged_source or receipt.is_ai_enabled})
    local goal = nil
    if job.distance and not job.staged_source then
        local world = living(job, job.victim)
        for _, npc in ipairs(world.npcs) do
            if npc.object_id == receipt.object_id then
                for _, direction in ipairs({{1,0}, {-1,0}, {0,1}, {0,-1},
                    {0.7071,0.7071}, {-0.7071,0.7071}, {0.7071,-0.7071}, {-0.7071,-0.7071}}) do
                    local requested = {x = npc.position.x + direction[1] * job.distance,
                        y = npc.position.y + direction[2] * job.distance, z = npc.position.z}
                    local path = must(job.victim.World:Path(npc.position, requested))
                    if path.is_available and path.is_complete and path.goal and path.goal.distance <= 0.75 then
                        goal = path.goal.position
                        break
                    end
                end
                if not goal then error("pending action has no complete positioning path") end
                must(job.victim.World:PlaceHero(goal))
                must(job.observer.World:PlaceHero(goal))
            end
        end
        if not goal then error("pending action source actor unavailable") end
    end
    for sample = 1, job.sample_count or (job.heal_owner and 30 or 12) do
        check(scenario.Sleep((job.heal_owner or job.fast_samples) and 1000 or 3000))
        if job.companion_slow and sample == 2 then moveOwner(job) end
        if job.heal_owner and sample == 9 then hitOwner(job, receipt.object_id) end
        if goal then
            must(job.victim.World:PlaceHero(goal))
            must(job.observer.World:PlaceHero(goal))
        end
        for _, client in ipairs({job.victim, job.observer}) do
            local world = living(job, client)
            log(job, "sample", {client = client.ID, sample = sample,
                hp = world.hero.hit_point, npc_count = #world.npcs,
                hero_x = world.hero.position.x, hero_y = world.hero.position.y})
            if job.companion_slow then
                local native = must(client.World:SnapshotNative()).gameplay_state
                log(job, "native_owner", {client = client.ID, sample = sample,
                    frame = native and native.frame_sequence,
                    x = native and native.position and native.position.x,
                    y = native and native.position and native.position.y})
            end
            for _, npc in ipairs(world.npcs) do
                if not npc.is_fixture and string.sub(string.lower(npc.noun), 1, 5) ~= "dest_" then
                log(job, "actor", {client = client.ID, sample = sample, object = npc.object_id,
                    noun = npc.noun, hp = npc.hit_point, is_dead = npc.is_dead,
                    x = npc.position.x, y = npc.position.y, z = npc.position.z})
                end
            end
        end
    end
    log(job, "capture_end", {object = receipt.object_id})
end
return function()
    check(scenario.RequireAPI(1))
    if #jobs == 0 then
        check(scenario.Log("npc_phase_sweep suite_end cases=0 status=no_pending_capture"))
        return
    end
    local server = must(scenario.StartServer({}))
    local branches = {}
    for _, job in ipairs(jobs) do
        prepare(server, job)
        branches[job.id] = function() run(job) end
    end
    check(scenario.Parallel(branches))
    for _, job in ipairs(jobs) do
        check(job.victim:Terminate())
        check(job.observer:Terminate())
        check(job.victimProfile:Logout())
        check(job.observerProfile:Logout())
    end
    check(scenario.Log("npc_phase_sweep suite_end cases=" .. #jobs))
end
