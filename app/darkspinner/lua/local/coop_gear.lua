---Two-client co-op saved-appearance and equipped-stat verification.
---Native editor save, party and squad switches; one controlled gunner uses normal combat.
local must = scenario.Must
local check = scenario.Check





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

local function saveNativeAppearance(client)
    must(client.Scene:Set(scene.Editor))
    check(scenario.Sleep(2500))
    must(client.UI:Hierarchy())
    must(client.Diagnostics:Capture("coop-editor-original"))
    must(client.UI:Click(640,690)) -- Native Paint tab, verified 1280x720 layout.
    check(scenario.Sleep(2500))
    must(client.UI:Click(54,315)) -- Installed orange skin-coat preset.
    check(scenario.Sleep(2500))
    must(client.Diagnostics:Capture("coop-editor-painted"))
    must(client.UI:Click(1217,694)) -- Native Accept: persists the real CRT and editor loadout.
    must(client.Scene:Wait(scene.Arsenal))
    must(client.Diagnostics:Capture("coop-appearance-saved"))
end

local function targetByID(worldSS, objectID)
    for _, npcSS in ipairs(worldSS.npcs) do
        if npcSS.object_id==objectID then return npcSS end
    end
    return nil
end

local function playMission(clients)
    inviteParty(clients)
    for _,client in ipairs(clients) do
        must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(clients[1].Mission:Select(1))
    must(clients[1].Mission:Start())
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Continue());return nil end,
        second=function() must(clients[2].Mission:Continue());return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Wait());return nil end,
        second=function() must(clients[2].Mission:Wait());return nil end,
    }))
    local firstSS=must(clients[1].World:Ready())
    local secondSS=must(clients[2].World:Ready())
    if firstSS.game_id==0 or firstSS.game_id~=secondSS.game_id then error("co-op game identity mismatch") end
    local expectedHPs={{220,300,215},{220,210,125}}
    captureBoth(clients,"coop-gear-entry")
    for slot=0,2 do
        if slot>0 then check(scenario.Sleep(5500)) end
        for participant,client in ipairs(clients) do
            must(client.World:SelectHero(slot))
            local worldSS=must(client.World:SnapshotLiving())
            local expectedHP=expectedHPs[participant][slot+1]
            check(scenario.Log("Co-op equipment client=" .. client.ID .. " slot=" .. slot
                .. " maximum_hp=" .. worldSS.hero.maximum_hit_point .. " expected=" .. expectedHP))
            if worldSS.hero.index~=slot or worldSS.hero.maximum_hit_point~=expectedHP then
                error("co-op equipment stat or selected hero differs from saved loadout")
            end
        end
        check(scenario.Sleep(2500))
        captureBoth(clients,"coop-gear-slot-" .. slot)
    end
    -- Focused equipment fixture: one AI-disabled gunner, ordinary shared
    -- publication/combat. Avoid an unrelated ambient route/death stopping the check.
    check(scenario.Sleep(5500))
    for _,client in ipairs(clients) do must(client.World:SelectHero(1)) end
    local npc=must(clients[1].World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local npcSS=must(npc:Snapshot())
    local beforeSS=must(clients[1].World:SnapshotLiving())
    local remoteSS=must(clients[2].World:SnapshotLiving())
    if not npcSS.is_available or npcSS.is_dead or npcSS.is_ai_enabled
        or #npcSS.supported_ability_names~=1 or targetByID(remoteSS,npc.ObjectID)==nil then
        error("controlled shared combat fixture unavailable on both clients")
    end
    must(npc:Move({x=beforeSS.hero.position.x,y=beforeSS.hero.position.y+3,z=beforeSS.hero.position.z}))
    local didTakeDamage=false
    for attempt=1,6 do
        must(npc:Cast(npcSS.supported_ability_names[1],beforeSS.hero.object_id))
        local deadline=scenario.Time()+3000
        while scenario.Time()<deadline do
            local afterSS=must(clients[1].World:SnapshotLiving())
            if afterSS.hero.hit_point<beforeSS.hero.hit_point then
                didTakeDamage=true
                check(scenario.Log("Equipped Vex incoming HP=" .. beforeSS.hero.hit_point .. " -> " .. afterSS.hero.hit_point))
                break
            end
            check(scenario.Sleep(150))
        end
        if didTakeDamage then break end
        check(scenario.Log("Settled gunner cast produced no damage; inspect dodge/miss packets"))
    end
    if not didTakeDamage then error("six settled ordinary gunner attacks did not damage equipped Vex") end
    captureBoth(clients,"coop-gear-incoming")
    check(scenario.Sleep(5500))
    for _,client in ipairs(clients) do must(client.World:SelectHero(2)) end
    for attempt=1,8 do
        local worldSS=must(clients[1].World:SnapshotLiving())
        local targetSS=targetByID(worldSS,npc.ObjectID)
        local remoteTargetSS=targetByID(must(clients[2].World:SnapshotLiving()),npc.ObjectID)
        if targetSS and remoteTargetSS and targetSS.is_dead and remoteTargetSS.is_dead then
            captureBoth(clients,"coop-gear-combat")
            check(scenario.Log("Co-op saved appearance, all squad slots, equipped HP, incoming damage and shared NPC death observed"))
            return nil
        end
        if targetSS==nil or remoteTargetSS==nil then error("shared NPC disappeared without a confirmed death") end
        local receipt=must(clients[1].World:Hold(targetSS.position,1000))
        local clickSS=receipt.gameplay_click
        if not receipt.is_processed or not clickSS or not clickSS.is_press_poll_observed
            or not clickSS.is_release_poll_observed then error("unpaired native attack; no replay") end
        check(scenario.Sleep(1500))
    end
    error("equipped SRS-42 did not produce a shared gunner death in eight processed attack windows")
end
return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local profiles={
        must(server:CreateProfile({name="coop-geared",hero_nouns={1667741389,3618328019,3524487169},chain_progression=1,
            equipped_parts={
                {squad_category="pve",hero_index=0,rigblock_asset_id=823,level=20,rarity=0},
                {squad_category="pve",hero_index=1,rigblock_asset_id=820,level=20,rarity=0},
                {squad_category="pve",hero_index=2,rigblock_asset_id=821,level=20,rarity=0},
                {squad_category="pve",hero_index=2,rigblock_asset_id=328,level=20,rarity=0},
            }})),
        must(server:CreateProfile({name="coop-bare",hero_nouns={1667741389,3618328019,3524487169},chain_progression=1})),
    }
    local clients={
        must(profiles[1]:Launch({client_id="coop-geared",launch_mode="normal"})),
        must(profiles[2]:Launch({client_id="coop-bare",launch_mode="detached"})),
    }
    for _,client in ipairs(clients) do must(client:Login());must(client.Scene:Wait(scene.Arsenal)) end
    saveNativeAppearance(clients[1])
    local isComplete,missionError=pcall(playMission,clients)
    if not isComplete then captureBoth(clients,"coop-gear-failed");error(tostring(missionError)) end
    return nil
end
