---One owned NPC, with autonomous AI disabled beforeWorldSS it enters the world.
---Manual actions use normal combat; publication alone never proves damage.
local must = scenario.Must
local check = scenario.Check

---@param client ManagedClient
---@param message string
local function fail(client, message)
    local capture, captureError = client.Diagnostics:Capture("controlled-npc-failed")
    return message .. "; diagnostic=" .. tostring(capture ~= nil and capture.manifest_path or captureError)
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name = "controlled", hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1,
    }))
    local client = must(profile:Launch({client_id = "controlled", launch_mode = "normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))

    local npc = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local initialNPCSS = must(npc:Snapshot())
    local beforeWorldSS = must(client.World:SnapshotLiving())
    local hero = beforeWorldSS.hero
    if hero == nil or not initialNPCSS.is_available or initialNPCSS.is_ai_enabled or initialNPCSS.is_dead
        or initialNPCSS.hit_point <= 0 or #initialNPCSS.supported_ability_names ~= 1 then
        return fail(client, "controlled spawn did not expose a living AI-disabled gunner and exact supported ability")
    end
    must(client.Diagnostics:Capture("controlled-npc-idleNPCSS"))
    check(scenario.Sleep(5000)) -- Longer than this gunner's ordinary shot cooldown.
    local idleNPCSS = must(npc:Snapshot())
    local idleWorldSS = must(client.World:SnapshotLiving())
    if idleWorldSS.hero == nil or idleNPCSS.is_ai_enabled or idleNPCSS.is_dead
        or idleNPCSS.action_generation ~= initialNPCSS.action_generation
        or idleNPCSS.position.x ~= initialNPCSS.position.x or idleNPCSS.position.y ~= initialNPCSS.position.y
        or idleNPCSS.position.z ~= initialNPCSS.position.z or idleWorldSS.hero.hit_point ~= hero.hit_point then
        return fail(client, "AI-disabled NPC acted, moved or damaged the hero during the idleNPCSS observation")
    end

    local destination = {x = initialNPCSS.position.x, y = initialNPCSS.position.y + 1, z = initialNPCSS.position.z}
    local moved = must(npc:Move(destination))
    if not moved.is_accepted or not moved.is_completed or moved.projected_destination == nil
        or moved.requested_destination == nil then
        return fail(client, "manual movement did not complete")
    end
    local positionedNPCSS = must(npc:Snapshot())
    local projected = moved.projected_destination
    if projected == nil then
        return fail(client, "manual movement omitted its resolved navigation goal")
    end
    if positionedNPCSS.is_ai_enabled or positionedNPCSS.position == nil
        or (positionedNPCSS.position.x == initialNPCSS.position.x and positionedNPCSS.position.y == initialNPCSS.position.y
            and positionedNPCSS.position.z == initialNPCSS.position.z) then
        return fail(client, "manual movement did not independently change the controlled NPC pose")
    end
    local deltaX = positionedNPCSS.position.x - projected.x
    local deltaY = positionedNPCSS.position.y - projected.y
    local deltaZ = positionedNPCSS.position.z - projected.z
    if deltaX * deltaX + deltaY * deltaY + deltaZ * deltaZ > 0.011 then
        return fail(client, "manual movement did not reach its reported projected goal")
    end
    local rejected, rejection = npc:Cast("unsupported-scenario-ability", hero.object_id)
    if rejection == nil or (rejected ~= nil and rejected.is_accepted) then
        return fail(client, "unsupported NPC ability was not rejected beforeWorldSS admission")
    end
    local cast = must(npc:Cast(initialNPCSS.supported_ability_names[1], hero.object_id))
    if not cast.is_accepted or not cast.is_settled then
        return fail(client, "manual gunner action did not settle")
    end
    local afterCastWorldSS = must(client.World:SnapshotLiving())
    if afterCastWorldSS.hero == nil or afterCastWorldSS.hero.hit_point >= hero.hit_point then
        return fail(client, "manual gunner cast completed without independently observed hero damage")
    end
    must(client.Diagnostics:Capture("controlled-npc-cast"))
    check(scenario.Sleep(5000))
    local afterWaitWorldSS = must(client.World:SnapshotLiving())
    if afterWaitWorldSS.hero == nil or afterWaitWorldSS.hero.hit_point ~= afterCastWorldSS.hero.hit_point then
        return fail(client, "manual gunner cast repeated while autonomous AI remained disabled")
    end

    must(npc:SetAI(true))
    local deadline = scenario.Time() + 10000
    local hasAutonomousDamage = false
    while scenario.Time() < deadline do
        local worldSS = must(client.World:SnapshotLiving())
        if worldSS.hero ~= nil and worldSS.hero.hit_point < afterWaitWorldSS.hero.hit_point then
            hasAutonomousDamage = true
            break
        end
        check(scenario.Sleep(200))
    end
    if not hasAutonomousDamage then
        return fail(client, "re-enabled gunner AI did not produce observed damage within ten seconds")
    end
    must(npc:SetAI(false))
    local stoppedNPCSS = must(npc:Snapshot())
    if stoppedNPCSS.is_ai_enabled then
        return fail(client, "AI disable did not settle")
    end
    must(client.Diagnostics:Capture("controlled-npc-stoppedNPCSS"))

    -- A stationary enemy also remains a real target for the player's ability.
    local result = must(client.Ability:Cast(stoppedNPCSS.position))
    local afterNPCSS = must(npc:Snapshot())
    if not result.is_completed or afterNPCSS.hit_point >= stoppedNPCSS.hit_point then
        return fail(client, "player ability did not independently damage the controlled enemy")
    end
    if afterNPCSS.is_dead then
        local deadCommand, deadError = npc:SetAI(true)
        if deadError == nil or (deadCommand ~= nil and deadCommand.is_accepted) then
            return fail(client, "dead controlled NPC accepted a mutation")
        end
    end
    must(client.Diagnostics:Capture("controlled-npc-player-ability"))
    return nil
end
