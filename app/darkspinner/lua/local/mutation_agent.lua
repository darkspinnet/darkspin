---Observe one Mutation Agent's build-103 presentation through normal campaign gameplay.
local must, check = scenario.Must, scenario.Check

local function distance(a, b)
    local x, y, z = a.x - b.x, a.y - b.y, a.z - b.z
    return math.sqrt(x*x + y*y + z*z)
end

local function actor(world, objectID)
    for _, npc in ipairs(world.npcs) do
        if npc.object_id == objectID then return npc end
    end
end

local function capture(client, label)
    check(scenario.Log("Mutation Agent: " .. label))
    must(client.UI:Screenshot(label))
end

local function observe(client, label, timeoutMs, predicate)
    local deadline = scenario.Time() + timeoutMs
    repeat
        local result = predicate(must(client.World:SnapshotLiving()))
        if result then return result end
        check(scenario.Sleep(40))
    until scenario.Time() >= deadline
    local evidence = client.Diagnostics:Capture(label .. "-timeout")
    error(label .. " was not observed; capture=" .. tostring(evidence and evidence.manifest_path))
end

local function capturePhase(client, objectID, phase, label, timeoutMs)
    observe(client, label, timeoutMs, function(world)
        local npc = actor(world, objectID)
        return npc and npc.mutation_phase == phase
    end)
    capture(client, label)
end

local function placeForHelper(client, world, agent)
    -- Focused spawns enter five units along +X from the hero. Leave the helper
    -- 8 units from the agent, inside the authored (5, 15] mutation window.
    for _, offset in ipairs({{x=0,y=8}, {x=0,y=-8}, {x=-8,y=0}, {x=8,y=0}}) do
        local requested = {x=agent.position.x+offset.x-5, y=agent.position.y+offset.y, z=agent.position.z}
        local path = must(client.World:Path(world.hero.position, requested))
        if path.is_available and path.is_complete and path.goal
            and distance(path.goal.position, requested) < 0.75 then
            local point = path.goal.position
            must(client.World:PlaceHero(point))
            observe(client, "helper-placement", 4000, function(current)
                return distance(current.hero.position, point) < 0.75
            end)
            return
        end
    end
    error("no reachable staging point for the Mutation Agent's ally")
end

local function defeatAgent(client, objectID)
    for _ = 1, 8 do
        local target = actor(must(client.World:SnapshotLiving()), objectID)
        if target and target.is_dead then return end
        if not target or not target.is_published then error("Mutation Agent disappeared before defeat") end
        local aim = {x=target.position.x, y=target.position.y, z=target.position.z+0.3}
        local input = must(client.World:Hold(aim, 150))
        if not input.is_processed or not input.gameplay_click or not input.gameplay_click.is_press_poll_observed
            or not input.gameplay_click.is_release_poll_observed then
            error("Mutation Agent attack input did not complete")
        end
        local after = observe(client, "agent-hit", 2500, function(world)
            local npc = actor(world, objectID)
            if npc and (npc.is_dead or npc.hit_point < target.hit_point) then return npc end
        end)
        if after.is_dead then return end
    end
    error("Mutation Agent survived eight confirmed attacks")
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "mutation-agent", hero_nouns = {709733043, 3376462303, 185979955}, chain_progression = 1,
        equipped_parts = {
            {squad_category="pve", hero_index=0, rigblock_asset_id=265, level=100, rarity=0},
            {squad_category="pve", hero_index=0, rigblock_asset_id=515, level=100, rarity=0},
        },
    }))
    local client = must(profile:Launch({client_id="mutation-agent", launch_mode="normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed=104729, is_ambient_population_suppressed=true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    must(client.World:Ready())

    local agentID = must(client.World:Spawn("MutationAgent.Noun")).object_id
    local idle = observe(client, "agent-idle", 5000, function(world)
        local npc = actor(world, agentID)
        if npc and npc.mutation_phase == "idle" then return npc end
    end)
    capture(client, "01-agent-idle")
    observe(client, "agent-wander", 8000, function(world)
        local npc = actor(world, agentID)
        return npc and npc.mutation_phase == "idle" and distance(idle.position, npc.position) >= 0.25
    end)
    capture(client, "02-agent-wander")

    local world = must(client.World:SnapshotLiving())
    local agent = actor(world, agentID)
    if not agent then error("Mutation Agent disappeared before ally staging") end
    placeForHelper(client, world, agent)
    local helper = must(client.World:SpawnControlledNPC("BabyMortar.Noun"))
    capturePhase(client, agentID, "windup", "03-agent-windup", 10000)
    capturePhase(client, agentID, "firing", "04-agent-firing", 4000)
    capturePhase(client, agentID, "channel", "05-agent-beam", 4000)
    capturePhase(client, helper.ObjectID, "growing", "06-ally-growth", 4000)
    capturePhase(client, agentID, "idle", "07-agent-return-to-idle", 4000)
    capturePhase(client, helper.ObjectID, "promoted", "08-ally-promoted", 4000)

    local retired = must(helper:Retire())
    if not retired.is_completed then error("Mutation Agent's ally did not retire") end
    defeatAgent(client, agentID)
    capture(client, "09-agent-defeated")
    observe(client, "agent-unpublished", 5000, function(current)
        local npc = actor(current, agentID)
        return not npc or not npc.is_published
    end)
    capture(client, "10-agent-removed")
end
