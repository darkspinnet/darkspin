---PvE regression for the shared chain and healing-channel execution used by Arena.
local must = scenario.Must
local check = scenario.Check

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({
        name="ability-family",hero_nouns={171910024,185979955,700180862},chain_progression=3,
    }))
    local client = must(profile:Launch({client_id="ability-family",launch_mode="normal"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(4))
    must(client.World:SelectHero(0))
    check(scenario.Sleep(1200))
    local primary = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local secondary = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local primaryBeforeSS = must(primary:Snapshot())
    local secondaryBeforeSS = must(secondary:Snapshot())
    if not primaryBeforeSS.is_available or not secondaryBeforeSS.is_available
        or primaryBeforeSS.is_dead or secondaryBeforeSS.is_dead
        or primaryBeforeSS.is_ai_enabled or secondaryBeforeSS.is_ai_enabled then
        error("chain regression requires two living AI-disabled NPCs")
    end
    local sourceSS = must(client.World:SnapshotNative()).gameplay_state
    local target={x=sourceSS.position.x+3,y=sourceSS.position.y,z=sourceSS.position.z}
    must(primary:Move(target))
    must(secondary:Move({x=target.x,y=target.y+2,z=target.z}))
    local arrivalDeadline=scenario.Time()+8000
    local isArrived=false
    while scenario.Time()<arrivalDeadline do
        primaryBeforeSS=must(primary:Snapshot())
        secondaryBeforeSS=must(secondary:Snapshot())
        local dx,dy=primaryBeforeSS.position.x-target.x,primaryBeforeSS.position.y-target.y
        local secondaryDX,secondaryDY=secondaryBeforeSS.position.x-target.x,secondaryBeforeSS.position.y-target.y-2
        if dx*dx+dy*dy<0.25 and secondaryDX*secondaryDX+secondaryDY*secondaryDY<0.25 then
            isArrived=true
            break
        end
        check(scenario.Sleep(100))
    end
    if not isArrived then error("controlled chain targets did not reach the native hero") end
    must(client.UI:Click(640,655))
    check(scenario.Sleep(150))
    must(client.World:Click({x=primaryBeforeSS.position.x,y=primaryBeforeSS.position.y,z=primaryBeforeSS.position.z+0.7}))
    check(scenario.Sleep(2200))
    local secondaryAfterSS = must(secondary:Snapshot())
    if not secondaryAfterSS.is_available or secondaryAfterSS.hit_point >= secondaryBeforeSS.hit_point then
        must(client.Diagnostics:Capture("pve-chain-failed"))
        error("shared chain did not damage the second NPC")
    end
    check(scenario.Log("PvE chain secondary HP " .. secondaryBeforeSS.hit_point .. " -> " .. secondaryAfterSS.hit_point))
    must(client.World:SelectHero(1))
    check(scenario.Sleep(1200))
    local beforeSS = must(client.World:SnapshotLiving())
    local nativeSS = must(client.World:SnapshotNative()).gameplay_state
    must(client.World:Click(nativeSS.position))
    must(client.UI:Click(670,655))
    check(scenario.Sleep(150))
    must(client.World:Click(nativeSS.position))
    check(scenario.Sleep(4300))
    local healedSS = must(client.World:SnapshotLiving())
    if healedSS.hero.object_id ~= beforeSS.hero.object_id
        or healedSS.hero.maximum_hit_point <= beforeSS.hero.maximum_hit_point
        or healedSS.hero.hit_point <= beforeSS.hero.hit_point then
        must(client.Diagnostics:Capture("pve-channel-failed"))
        error("shared healing channel did not increase health and capacity")
    end
    must(client.Diagnostics:Capture("pve-channel-complete"))
    check(scenario.Sleep(13000))
    local expiredSS = must(client.World:SnapshotLiving())
    if math.abs(expiredSS.hero.maximum_hit_point-beforeSS.hero.maximum_hit_point)>0.05
        or expiredSS.hero.hit_point > expiredSS.hero.maximum_hit_point+0.05 then
        error("shared healing capacity did not expire and clamp")
    end
    check(scenario.Log("PvE healing channel and capacity expiry passed"))
    must(client.Diagnostics:Capture("pve-channel-expired"))
    return nil
end
