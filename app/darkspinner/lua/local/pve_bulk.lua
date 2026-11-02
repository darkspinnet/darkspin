---One owned co-op world, all unique build-103 castable assets, no automatic input replay.
---Dispatch is server admission evidence; packet delivery and native rendering need later trace review.
local must,check=scenario.Must,scenario.Check
local selectedAssets={}
local startAt=1
local cases={
    {noun=43291678,hero="Arakna Gamma",slot="basic",ability_id=76334808,asset="SoulRavagerBasic",kind="projectile"},
    {noun=43291678,hero="Arakna Gamma",slot="random",ability_id=3176239913,asset="NecroRandom",kind="channel_drain"},
    {noun=43291678,hero="Arakna Gamma",slot="special_one",ability_id=3168159823,asset="SoulRavagerSupport",kind="projectile_burst"},
    {noun=43291678,hero="Arakna Gamma",slot="special_two",ability_id=12116216,asset="SoulRavagerActive",kind="projectile_burst"},
    {noun=117394854,hero="Arborus Delta",slot="basic",ability_id=75936816,asset="SplinteringCleave",kind="melee"},
    {noun=117394854,hero="Arborus Delta",slot="random",ability_id=1577880566,asset="CastEnrage",kind="targeted_aoe"},
    {noun=117394854,hero="Arborus Delta",slot="special_one",ability_id=1806152134,asset="ArborealMight",kind="modifier"},
    {noun=117394854,hero="Arborus Delta",slot="special_two",ability_id=3519716422,asset="EntanglingRush",kind="charge"},
    {noun=171910024,hero="Goliath Gamma",slot="basic",ability_id=76648696,asset="EnergySentinelBasic",kind="melee"},
    {noun=171910024,hero="Goliath Gamma",slot="random",ability_id=1716347456,asset="ClaymoreTrap",kind="trap"},
    {noun=171910024,hero="Goliath Gamma",slot="special_one",ability_id=3634657371,asset="EnergySentinelActive",kind="melee"},
    {noun=171910024,hero="Goliath Gamma",slot="special_two",ability_id=474392318,asset="EnergySentinelSupport",kind="melee"},
    {noun=185979955,hero="Meditron Beta",slot="basic",ability_id=76539968,asset="FieldMedicBasic",kind="projectile"},
    {noun=185979955,hero="Meditron Beta",slot="random",ability_id=3424520656,asset="TechRandom",kind="cursor_area"},
    {noun=185979955,hero="Meditron Beta",slot="special_one",ability_id=2778725896,asset="FieldMedicSupport",kind="healing_ticks"},
    {noun=185979955,hero="Meditron Beta",slot="special_two",ability_id=2523327653,asset="FieldMedicActive",kind="modifier_area"},
    {noun=239643518,hero="Skar Gamma",slot="basic",ability_id=76307608,asset="ShadowRavagerBasic",kind="melee"},
    {noun=239643518,hero="Skar Gamma",slot="random",ability_id=1505211445,asset="CastSoulLink",kind="modifier"},
    {noun=239643518,hero="Skar Gamma",slot="special_one",ability_id=1938450222,asset="ShadowRavagerSupport",kind="modifier"},
    {noun=239643518,hero="Skar Gamma",slot="special_two",ability_id=2585949419,asset="ShadowRavagerActive",kind="melee"},
    {noun=308338531,hero="Tork Gamma",slot="basic",ability_id=75901384,asset="Sprout",kind="cloud_lob"},
    {noun=308338531,hero="Tork Gamma",slot="random",ability_id=3945174050,asset="BioRandom2",kind="charge"},
    {noun=308338531,hero="Tork Gamma",slot="special_one",ability_id=3416820780,asset="Sporogenesis",kind="healing_ticks"},
    {noun=308338531,hero="Tork Gamma",slot="special_two",ability_id=3124235417,asset="SleepingCloud",kind="aura_area"},
    {noun=454054015,hero="Blitz Beta",slot="basic",ability_id=75786000,asset="LightningRogueBasic",kind="melee"},
    {noun=454054015,hero="Blitz Beta",slot="random",ability_id=1479879510,asset="PlasmaRandom",kind="cursor_area"},
    {noun=454054015,hero="Blitz Beta",slot="special_one",ability_id=2779439490,asset="LightningRogueSupport",kind="point_blank"},
    {noun=454054015,hero="Blitz Beta",slot="special_two",ability_id=1137096183,asset="LightningRogueActive",kind="teleport_strike"},
    {noun=507464763,hero="Andromeda Delta",slot="basic",ability_id=76392248,asset="ForcePulse",kind="projectile"},
    {noun=507464763,hero="Andromeda Delta",slot="random",ability_id=4213688287,asset="SpacetimeRandom",kind="status_area"},
    {noun=507464763,hero="Andromeda Delta",slot="special_one",ability_id=2139808077,asset="RepulsionWave",kind="repulsion"},
    {noun=507464763,hero="Andromeda Delta",slot="special_two",ability_id=1634946936,asset="GravityStorm",kind="channel_area"},
    {noun=576185321,hero="Zrin Alpha",slot="basic",ability_id=75806856,asset="PlasmaSentinelBasic",kind="melee"},
    {noun=576185321,hero="Zrin Alpha",slot="special_one",ability_id=657264656,asset="PlasmaSentinelSupport",kind="timed_area"},
    {noun=576185321,hero="Zrin Alpha",slot="special_two",ability_id=2664879357,asset="PlasmaSentinelActive",kind="reactive_summon"},
    {noun=577654149,hero="Krel Delta",slot="basic",ability_id=75710184,asset="FireRavagerBasic",kind="projectile"},
    {noun=577654149,hero="Krel Delta",slot="random",ability_id=3492557026,asset="PlasmaRandom_LightningBall",kind="projectile"},
    {noun=577654149,hero="Krel Delta",slot="special_one",ability_id=2779429002,asset="FireRavagerSupport",kind="projectile_burst"},
    {noun=577654149,hero="Krel Delta",slot="special_two",ability_id=2030431727,asset="FireRavagerActive",kind="projectile"},
    {noun=647027626,hero="Tork Beta",slot="random",ability_id=3912788931,asset="SummonSprite",kind="summon_buff"},
    {noun=700180862,hero="Lumin Gamma",slot="basic",ability_id=75734096,asset="LightningTempest_Basic",kind="projectile"},
    {noun=700180862,hero="Lumin Gamma",slot="random",ability_id=2410492198,asset="PlasmaRandom_FireWave",kind="projectile"},
    {noun=700180862,hero="Lumin Gamma",slot="special_one",ability_id=1202681981,asset="LightningTempest_Support",kind="melee"},
    {noun=700180862,hero="Lumin Gamma",slot="special_two",ability_id=133451726,asset="LightningTempest_Active",kind="projectile_burst"},
    {noun=709733043,hero="Char Delta",slot="basic",ability_id=75763312,asset="FireTempestBasic",kind="cursor_area"},
    {noun=709733043,hero="Char Delta",slot="special_one",ability_id=3139436766,asset="FireTempestSupport",kind="targeted_aoe"},
    {noun=709733043,hero="Char Delta",slot="special_two",ability_id=2087191227,asset="FireTempestActive",kind="summon_buff"},
    {noun=713294352,hero="Viper Delta",slot="basic",ability_id=75874704,asset="LFPoisonRavager_Stab",kind="melee"},
    {noun=713294352,hero="Viper Delta",slot="special_one",ability_id=802116456,asset="LFPoisonRavager_PoisonNova",kind="point_blank"},
    {noun=713294352,hero="Viper Delta",slot="special_two",ability_id=3325683662,asset="LFPoisonRavager_Expunge",kind="melee"},
    {noun=725345831,hero="Orion Gamma",slot="basic",ability_id=76422400,asset="LightspeedTempestBasic",kind="projectile"},
    {noun=725345831,hero="Orion Gamma",slot="random",ability_id=1050649660,asset="SpacetimeRandom1",kind="projectile"},
    {noun=725345831,hero="Orion Gamma",slot="special_one",ability_id=755823803,asset="LightspeedTempestSupport",kind="projectile"},
    {noun=725345831,hero="Orion Gamma",slot="special_two",ability_id=819783980,asset="LightspeedTempestActive",kind="cursor_area"},
    {noun=749013658,hero="Sage Alpha",slot="basic",ability_id=75836024,asset="SupportHealerBasic",kind="projectile"},
    {noun=749013658,hero="Sage Alpha",slot="special_one",ability_id=1829107826,asset="SupportHealerSupport",kind="targeted_aoe"},
    {noun=749013658,hero="Sage Alpha",slot="special_two",ability_id=424126604,asset="TreeOfLife",kind="area_healing"},
    {noun=753829266,hero="Wraith Gamma",slot="basic",ability_id=76361448,asset="Pummel",kind="melee"},
    {noun=753829266,hero="Wraith Gamma",slot="random",ability_id=4071514749,asset="AfflictionBolt",kind="projectile_status"},
    {noun=753829266,hero="Wraith Gamma",slot="special_one",ability_id=638735652,asset="Ghostform",kind="point_blank"},
    {noun=753829266,hero="Wraith Gamma",slot="special_two",ability_id=2457641635,asset="DeathsEmbrace",kind="point_blank"},
    {noun=758141691,hero="Revenant Delta",slot="basic",ability_id=76251664,asset="Weakness",kind="projectile"},
    {noun=758141691,hero="Revenant Delta",slot="special_one",ability_id=805983062,asset="Terrify",kind="projectile_status"},
    {noun=758141691,hero="Revenant Delta",slot="special_two",ability_id=2360918190,asset="Psistorm",kind="aura_area"},
    {noun=820281267,hero="Seraph-XS Alpha",slot="basic",ability_id=76586288,asset="Trapper_Grenade",kind="toss"},
    {noun=820281267,hero="Seraph-XS Alpha",slot="special_one",ability_id=1402151322,asset="TurretTrap",kind="trap"},
    {noun=820281267,hero="Seraph-XS Alpha",slot="special_two",ability_id=42036489,asset="PipeBomb",kind="trap"},
    {noun=962104613,hero="Zrin Delta",slot="random",ability_id=3822161836,asset="PlasmaRandom_WebbedLightning",kind="projectile_burst"},
    {noun=1014804343,hero="Skar Beta",slot="random",ability_id=507754556,asset="PhantomCharge",kind="charge"},
    {noun=1077715391,hero="Vex Delta",slot="basic",ability_id=76484880,asset="TimeRavagerBasic",kind="melee"},
    {noun=1077715391,hero="Vex Delta",slot="special_one",ability_id=3599332149,asset="TimeRavagerSupport",kind="teleport_area"},
    {noun=1077715391,hero="Vex Delta",slot="special_two",ability_id=566463862,asset="TimeRavagerActive",kind="point_blank"},
    {noun=1194511925,hero="Titan Beta",slot="basic",ability_id=76619280,asset="TCShieldedSentinelBasic",kind="point_blank"},
    {noun=1194511925,hero="Titan Beta",slot="random",ability_id=592015938,asset="TechRandom2",kind="modifier"},
    {noun=1194511925,hero="Titan Beta",slot="special_one",ability_id=1124039199,asset="TCShieldedSentinelSupport",kind="cursor_area"},
    {noun=1194511925,hero="Titan Beta",slot="special_two",ability_id=2253235688,asset="TCShieldedSentinelActive",kind="point_blank"},
    {noun=1237326306,hero="Viper Alpha",slot="random",ability_id=4200701581,asset="RootingPlague",kind="infection"},
    {noun=1240682488,hero="Maldri Delta",slot="basic",ability_id=76455920,asset="QuantumStrike",kind="melee"},
    {noun=1240682488,hero="Maldri Delta",slot="special_one",ability_id=2577488467,asset="QuantumState",kind="modifier"},
    {noun=1240682488,hero="Maldri Delta",slot="special_two",ability_id=1587665566,asset="QuantumBlink",kind="quantum_blink"},
    {noun=1264710567,hero="Savage Gamma",slot="basic",ability_id=76223592,asset="BeastSmash",kind="melee"},
    {noun=1264710567,hero="Savage Gamma",slot="special_one",ability_id=1018662501,asset="SummonBeast",kind="summon_buff"},
    {noun=1264710567,hero="Savage Gamma",slot="special_two",ability_id=2414469030,asset="BeastCharge",kind="charge"},
    {noun=1442915581,hero="Jinx Beta",slot="basic",ability_id=76280984,asset="VoodooTempestBasic",kind="toss"},
    {noun=1442915581,hero="Jinx Beta",slot="special_one",ability_id=218130412,asset="VoodooTempestSupport",kind="status_area"},
    {noun=1442915581,hero="Jinx Beta",slot="special_two",ability_id=4198537489,asset="VoodooTempestActive",kind="channel_drain"},
    {noun=1476711747,hero="Magnos Delta",slot="basic",ability_id=76513592,asset="BinarySentinelBasic",kind="melee"},
    {noun=1476711747,hero="Magnos Delta",slot="random",ability_id=1050649663,asset="SpacetimeRandom2",kind="aura_area"},
    {noun=1476711747,hero="Magnos Delta",slot="special_one",ability_id=1689854343,asset="BinarySentinelSupport",kind="cone"},
    {noun=1476711747,hero="Magnos Delta",slot="special_two",ability_id=4079625408,asset="BinarySentinelActive",kind="cone"},
    {noun=1557965449,hero="Meditron Alpha",slot="random",ability_id=592015939,asset="TechRandom3",kind="melee"},
    {noun=2038499613,hero="SRS-42 Delta",slot="basic",ability_id=76569224,asset="MissileTempestBasic",kind="projectile"},
    {noun=2038499613,hero="SRS-42 Delta",slot="special_one",ability_id=1187084208,asset="MissileTempestSupport",kind="cursor_area"},
    {noun=2038499613,hero="SRS-42 Delta",slot="special_two",ability_id=4040466205,asset="MissileTempestActive",kind="projectile_burst"},
    {noun=2902294899,hero="Maldri Gamma",slot="random",ability_id=1050649662,asset="SpacetimeRandom3",kind="cursor_area"},
}
local function pause(ms) check(scenario.Sleep(ms)) end
local function jsonString(item)
    return '"'..tostring(item):gsub('[%z\1-\31\\"]',function(character)
        return string.format("\\u%04x",string.byte(character))
    end)..'"'
end
local function json(item)
    if item==nil then return "null" end
    if type(item)=="string" then return jsonString(item) end
    if type(item)=="number" or type(item)=="boolean" then return tostring(item) end
    local keys,entries={},{}
    for key in pairs(item) do keys[#keys+1]=key end
    table.sort(keys)
    for _,key in ipairs(keys) do entries[#entries+1]=jsonString(key)..":"..json(item[key]) end
    return "{"..table.concat(entries,",").."}"
end
local function record(event)
    event.suite,event.at_ms="pve_bulk",scenario.Time()
    local line=json(event)
    if #line>2048 then error("bulk status record exceeds durable boundary") end
    check(scenario.Log(line))
end
local function selected(case,index)
    return index>=startAt and (next(selectedAssets)==nil or selectedAssets[case.asset]==true)
end
local function launch(server,index)
    local id=index==1 and "pve-bulk-source" or "pve-bulk-observer"
    local noun=index==1 and 1667741389 or 700180862
    local profile=must(server:CreateProfile({name=id,hero_nouns={noun,117394854,576185321},chain_progression=1}))
    local client=must(profile:Launch({client_id=id,launch_mode=index==1 and "normal" or "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    local window=must(client.UI:InspectWindow()).window
    if not window or window.width~=1280 or window.height~=720 then error("bulk hotbar requires 1280x720 owned windows") end
    record({event="client",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,initial_noun=noun})
    return client
end
local function menu(client,x,y)
    local receipt,clickError=client.UI:Click(x,y)
    if not receipt and string.find(tostring(clickError),
        "native_rejected: owned window rejected the queued command",1,true) then
        pause(600)
        receipt,clickError=client.UI:Click(x,y)
    end
    if not receipt then error("native menu click failed: "..tostring(clickError)) end
    if not receipt.is_processed then error("native Social input unprocessed; no replay") end
    pause(900)
end
local function enter(clients)
    local leader,follower=clients[1],clients[2]
    menu(leader,105,35)
    menu(leader,368,182)
    menu(leader,930,265)
    menu(follower,604,53)
    local deadline=scenario.Time()+12000
    local isPartyReady=false
    while scenario.Time()<deadline do
        local first=must(leader.Party:Snapshot()).party
        local second=must(follower.Party:Snapshot()).party
        if first.id~=0 and first.id==second.id and #first.members==2 and #second.members==2 then
            isPartyReady=true
            break
        end
        pause(150)
    end
    if not isPartyReady then error("owned native Social party unavailable") end
    pause(900)
    menu(leader,759,498)
    menu(leader,1036,103)
    for _,client in ipairs(clients) do
        must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(leader.Mission:Select(1))
    must(leader.Mission:Start())
    check(scenario.Parallel({
        first=function() must(leader.Mission:Continue());return nil end,
        second=function() must(follower.Mission:Continue());return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(leader.Mission:Wait());return nil end,
        second=function() must(follower.Mission:Wait());return nil end,
    }))
end
local function read(client,baseline)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if not world.is_available or not world.hero or not world.campaign or #world.campaign.members~=2
        or world.zone_generation==0 or not native or not native.is_world_active or not native.is_rig_ready
        or probe.pid~=client.PID or probe.instance_id~=client.InstanceID then
        error("owned co-op world or native rig unavailable")
    end
    if baseline and (world.game_id~=baseline.game_id or world.zone_generation~=baseline.zone_generation
        or world.peer_generation~=baseline.peer_generation or world.transport_generation~=baseline.transport_generation
        or world.user_id~=baseline.user_id) then error("owned campaign epoch changed; no dependent input") end
    return world,native
end
local function roster(clients,baselines,noun)
    for index,client in ipairs(clients) do
        local world=read(client,baselines[index])
        for participant,baseline in ipairs(baselines) do
            local found=false
            for _,member in ipairs(world.campaign.members) do
                if member.user_id==baseline.user_id then
                    if found or not member.is_ready or member.zone_generation~=baseline.zone_generation
                        or member.peer_generation~=baseline.peer_generation
                        or member.transport_generation~=baseline.transport_generation then
                        error("co-op roster epoch or readiness changed")
                    end
                    if participant==1 then
                        local active
                        for _,hero in ipairs(member.characters) do
                            if hero.index==0 then active=hero end
                        end
                        if not active or active.noun~=noun or active.is_defeated or active.hit_point<=0 then
                            error("replacement noun missing from an observer roster")
                        end
                    end
                    found=true
                end
            end
            if not found then error("co-op roster omitted an owned participant") end
        end
    end
end
local function isHealingCase(case)
    return case.kind=="healing_ticks" or case.kind=="area_healing"
        or case.asset=="SupportHealerBasic" or case.asset=="SoulRavagerSupport"
end
local function prepareHero(clients,baselines,case)
    local source,observer=clients[1],clients[2]
    local replacement=must(source.World:ReplaceHero(case.noun))
    if not replacement.is_replacement or not replacement.is_rig_ready or replacement.recipient_count~=2
        or replacement.zone_generation~=baselines[1].zone_generation then
        error("campaign hero replacement incomplete")
    end
    roster(clients,baselines,case.noun)
    for _,client in ipairs(clients) do
        local restored=must(client.World:RestoreHero())
        local cooldown=must(client.World:ResetCooldowns())
        if not restored.is_queued or restored.recipient_count~=2 or not cooldown.is_cooldown_reset
            or not cooldown.is_queued or cooldown.recipient_count~=2 then
            error("campaign resource/cooldown fixture incomplete")
        end
    end
    if isHealingCase(case) then
        for index,client in ipairs(clients) do
            local world=read(client,baselines[index])
            local wounded=math.max(1,math.floor(world.hero.maximum_hit_point*0.5))
            local receipt=must(client.World:RestoreHero({hit_point=wounded}))
            if not receipt.is_queued then error("wounded healing baseline was not queued") end
        end
    end
end
local function prepareTarget(source,npc)
    if npc then
        local current,readError=npc:Snapshot()
        if not current then return nil,"targetSnapshot: "..tostring(readError) end
        if current.is_available and not current.is_dead and current.hit_point>0 then
            local restored,restoreError=npc:Restore()
            if not restored then return nil,"targetRestore: "..tostring(restoreError) end
            if not restored.is_accepted or not restored.is_completed or not restored.is_settled
                or not restored.is_response_committed then return nil,"targetRestore: unsettled receipt" end
            local after,afterError=npc:Snapshot()
            if not after or not after.is_available or after.is_dead or after.hit_point<=0
                or after.identity.object_id~=current.identity.object_id then
                return nil,"targetRestoreState: "..tostring(afterError)
            end
            return npc,nil
        end
    end
    local created,spawnError=source.World:SpawnControlledNPC("CitadelBasicGunner.Noun")
    if not created then return nil,"targetSpawn: "..tostring(spawnError) end
    local current,readError=created:Snapshot()
    if not current or not current.is_available or current.is_dead or current.is_ai_enabled then
        return nil,"targetSpawnState: "..tostring(readError)
    end
    return created,nil
end
local function targetMode(case)
    if case.asset=="CastEnrage" or case.asset=="ArborealMight" or case.asset=="CastSoulLink"
        or case.asset=="FieldMedicSupport" or case.asset=="FieldMedicActive"
        or case.asset=="Sporogenesis" or case.asset=="TreeOfLife" or case.asset=="Ghostform"
        or case.asset=="QuantumState" or case.asset=="SummonBeast" or case.asset=="SummonSprite"
        or case.kind=="point_blank" or case.kind=="repulsion" or case.kind=="reactive_summon"
        or case.kind=="modifier" or case.kind=="summon_buff" or case.kind=="area_healing" then
        return "self"
    end
    return "npc"
end
local function prepareRange(clients,baselines,case,npc)
    local isTargetFixture=case.kind=="melee" or case.kind=="point_blank" or case.kind=="cone"
        or case.kind=="toss" or case.kind=="repulsion" or case.kind=="charge"
        or case.kind=="projectile" or case.kind=="projectile_status"
        or case.kind=="projectile_burst" or case.kind=="cloud_lob"
        or case.kind=="infection" or case.kind=="cursor_area"
        or case.kind=="teleport_strike"
    if not isTargetFixture then return end
    local target=must(npc:Snapshot())
    if not target.is_available or target.is_dead then error("close-range target unavailable") end
    local world,native=read(clients[1],baselines[1])
    if world.hero.noun~=case.noun then error("close-range actor changed before target staging") end
    local goal={x=native.position.x+2.5,y=native.position.y,z=native.position.z}
    local moved=must(npc:Move(goal))
    if not moved.is_accepted or not moved.is_completed or not moved.projected_destination then
        error("close-range controlled target movement incomplete")
    end
    target=must(npc:Snapshot())
    local dx=native.position.x-target.position.x
    local dy=native.position.y-target.position.y
    local distance=math.sqrt(dx*dx+dy*dy)
    if not target.is_available or target.is_dead or distance>3.5 or distance<0.5 then
        error("close-range target did not settle beside native actor: distance="..tostring(distance))
    end
end
local function aimPoint(mode,clients,npc)
    if mode=="self" or mode=="ally" then
        local client=mode=="self" and clients[1] or clients[2]
        local native=must(client.World:SnapshotNative()).gameplay_state
        if not native or not native.is_rig_ready or not native.position then error("native aim actor unavailable") end
        return {x=native.position.x,y=native.position.y,z=native.position.z+1.5}
    end
    local target=must(npc:Snapshot())
    if not target.is_available or target.is_dead or target.hit_point<=0 then error("controlled target died before input") end
    return {x=target.position.x,y=target.position.y,z=target.position.z+1.5}
end
local function groundAim(source)
    local native=must(source.World:SnapshotNative()).gameplay_state
    if not native or not native.is_rig_ready or not native.position then
        error("native ground aim actor unavailable")
    end
    for _,offset in ipairs({{4,0},{0,4},{-4,0},{0,-4}}) do
        local goal={x=native.position.x+offset[1],y=native.position.y+offset[2],z=native.position.z}
        local path=must(source.World:Path(native.position,goal))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal then
            return path.goal.position
        end
    end
    error("ground aim lacks a complete native path")
end
local function dispatch(case,clients,npc)
    local source=clients[1]
    local aim=aimPoint(targetMode(case),clients,npc)
    if case.kind=="trap" or case.kind=="quantum_blink" or case.kind=="teleport_area" then
        aim=groundAim(source)
    end
    local projection=must(source.World:Project(aim)).projection
    if not projection or not projection.is_available or not projection.is_on_screen then
        error("native aim projection off screen before input; no edge submitted")
    end
    if case.slot=="basic" then
        local receipt,inputError=source.World:Hold(aim,150)
        if not receipt and string.find(tostring(inputError),
            "unavailable: native UI operation is not currently eligible or available",1,true) then
            record({event="native_refusal",asset=case.asset,operation="basic_hold",is_edge_submitted=false})
            pause(400)
            local world,native=read(source)
            if world.hero.noun~=case.noun or not native.is_rig_ready then
                error("basic native refusal followed by changed actor; no retry")
            end
            receipt,inputError=source.World:Hold(aim,150)
        end
        if not receipt then error("basic native hold failed: "..tostring(inputError)) end
        local click=receipt.gameplay_click
        if not receipt.is_processed or not click or not click.is_press_poll_observed
            or not click.is_release_poll_observed then
            error("basic native hold lacked paired polls; no replay")
        end
        record({event="native_input",asset=case.asset,operation="basic_hold",
            retained_handle=click and click.retained_handle or nil,
            is_ground_query_success=click and click.is_native_ground_query_success or false,
            ray_point=click and click.ray_point or nil})
        return
    end
    local x=case.slot=="random" and 605 or case.slot=="special_one" and 640 or 575
    local button=must(source.UI:Click(x,655))
    if not button.is_processed then error("native hotbar edge unprocessed; no replay") end
    pause(300)
    local receipt=must(source.World:Click(aim))
    if not receipt.is_processed then error("native world ability edge unprocessed; no replay") end
    local click=receipt.gameplay_click
    record({event="native_input",asset=case.asset,operation="ability_click",
        retained_handle=click and click.retained_handle or nil,
        is_ground_query_success=click and click.is_native_ground_query_success or false,
        ray_point=click and click.ray_point or nil})
end
local function runCast(clients,baselines,case,npc)
    local source,observer=clients[1],clients[2]
    local beforeSource,beforeSourceNative=read(source,baselines[1])
    local beforeObserver,beforeObserverNative=read(observer,baselines[2])
    local beforeTarget=must(npc:Snapshot())
    if beforeSource.hero.noun~=case.noun or beforeTarget.is_dead or not beforeTarget.is_available
        or beforeTarget.is_ai_enabled or not beforeSource.ability then error("cast baseline unavailable") end
    local previous=beforeSource.ability.last_dispatch and beforeSource.ability.last_dispatch.revision or 0
    record({event="case_start",asset=case.asset,hero=case.hero,slot=case.slot,kind=case.kind,
        ability_id=case.ability_id,noun=case.noun,game_id=beforeSource.game_id,
        zone_generation=beforeSource.zone_generation,source_pid=source.PID,observer_pid=observer.PID,
        source_object_id=beforeSource.hero.object_id,target_object_id=beforeTarget.identity.object_id,
        target_hp=beforeTarget.hit_point,source_frame=beforeSourceNative.frame_sequence,
        observer_frame=beforeObserverNative.frame_sequence,previous_dispatch_revision=previous})
    dispatch(case,clients,npc)
    local settle=1800
    if case.kind=="projectile" or case.kind=="projectile_status" or case.kind=="cloud_lob" then settle=3500 end
    if case.asset=="AfflictionBolt" then settle=10000 end
    if case.kind=="projectile_burst" or case.kind=="channel_drain" or case.kind=="channel_area" then settle=6000 end
    if case.kind=="healing_ticks" or case.kind=="area_healing" or case.kind=="timed_area" then settle=3500 end
    if case.kind=="trap" or case.kind=="summon_buff" or case.kind=="reactive_summon" then settle=3000 end
    if case.asset=="LightningTempest_Active" then settle=32000 end
    local deadline=scenario.Time()+math.max(8000,settle+5000)
    local acceptedAt
    local afterSource,afterObserver,afterTarget,afterSourceNative,afterObserverNative
    local observed
    while scenario.Time()<deadline do
        afterSource,afterSourceNative=read(source,baselines[1])
        afterObserver,afterObserverNative=read(observer,baselines[2])
        afterTarget=must(npc:Snapshot())
        if afterSource.hero.noun~=case.noun or afterSource.hero.object_id~=beforeSource.hero.object_id
            or afterObserver.hero.object_id~=beforeObserver.hero.object_id then
            error("cast actor changed before outcome")
        end
        local current=afterSource.ability and afterSource.ability.last_dispatch
        if current and current.revision>previous then
            if current.revision~=previous+1 then error("server dispatch revision skipped; outcome ambiguous") end
            if current.actor_object_id~=beforeSource.hero.object_id or current.ability_id~=case.ability_id
                or current.ability_asset~=case.asset or current.slot~=case.slot then
                error("server dispatch identity disagrees with native case")
            end
            observed=current
            acceptedAt=acceptedAt or scenario.Time()
        end
        if observed and afterSourceNative.frame_sequence>beforeSourceNative.frame_sequence
            and afterObserverNative.frame_sequence>beforeObserverNative.frame_sequence then
            if scenario.Time()-acceptedAt>=settle then break end
        end
        pause(100)
    end
    if not observed then error("native input lacked exact server dispatch within bounded window; no replay") end
    if not afterSourceNative or afterSourceNative.frame_sequence<=beforeSourceNative.frame_sequence
        or not afterObserverNative or afterObserverNative.frame_sequence<=beforeObserverNative.frame_sequence then
        error("caster or observer native frame did not advance")
    end
    local isTargetDamage=afterTarget.hit_point<beforeTarget.hit_point
    local isSourceHeal=afterSource.hero.hit_point>beforeSource.hero.hit_point
    local isObserverHeal=afterObserver.hero.hit_point>beforeObserver.hero.hit_point
    local evidence=(isTargetDamage or isSourceHeal or isObserverHeal) and "server_resource_effect" or "server_dispatch_only"
    record({event="case_observation",asset=case.asset,evidence=evidence,
        dispatch_revision=observed.revision,dispatch_slot=observed.slot,
        dispatch_target_object_id=observed.target_object_id,
        target_hp_before=beforeTarget.hit_point,target_hp_after=afterTarget.hit_point,
        source_hp_before=beforeSource.hero.hit_point,source_hp_after=afterSource.hero.hit_point,
        observer_hp_before=beforeObserver.hero.hit_point,observer_hp_after=afterObserver.hero.hit_point,
        source_frame_after=afterSourceNative.frame_sequence,observer_frame_after=afterObserverNative.frame_sequence,
        is_packet_delivery_verified=false,is_native_rendering_verified=false})
    return evidence
end
local function capture(clients,label)
    for _,client in ipairs(clients) do
        local receipt,captureError=client.Diagnostics:Capture(label)
        record({event="diagnostic",label=label,client_id=client.ID,pid=client.PID,
            manifest=receipt and receipt.manifest_path or nil,
            failure=captureError and string.sub(tostring(captureError),1,300) or nil})
    end
end
return function()
    check(scenario.RequireAPI(1))
    local expected=0
    for index,case in ipairs(cases) do
        if selected(case,index) then
            expected=expected+1
            record({event="case_manifest",asset=case.asset,hero=case.hero,noun=case.noun,
                slot=case.slot,kind=case.kind,ability_id=case.ability_id,
                is_packet_delivery_verified=false,is_native_rendering_verified=false})
        end
    end
    if expected==0 then error("no selected ability assets") end
    local server=must(scenario.StartServer({}))
    local clients={launch(server,1),launch(server,2)}
    enter(clients)
    local baselines={}
    for index,client in ipairs(clients) do
        must(client.World:Ready())
        baselines[index]=read(client)
    end
    if baselines[1].game_id~=baselines[2].game_id
        or baselines[1].zone_generation~=baselines[2].zone_generation then
        error("owned co-op clients entered different campaign worlds")
    end
    capture(clients,"pve-bulk-entry")
    local npc,admitted,effects= nil,0,0
    local isQuarantined=false
    local isFailureCaptured=false
    for index,case in ipairs(cases) do
        if selected(case,index) then
            if isQuarantined then
                record({event="retry",asset=case.asset,reason="co-op pair quarantined after uncertain or failed case",is_replayed=false})
            else
                local setupFailure=scenario.Parallel({
                    setup=function() prepareHero(clients,baselines,case);return nil end,
                })
                if setupFailure then
                    record({event="case_end",asset=case.asset,status="retry_required",phase="setup",
                        failure=string.sub(tostring(setupFailure),1,600),is_replayed=false})
                    isQuarantined=true
                    capture(clients,"pve-bulk-first-failure")
                else
                    local target,targetError=prepareTarget(clients[1],npc)
                    if not target then
                        record({event="case_end",asset=case.asset,status="retry_required",phase="target",
                            failure=targetError,is_replayed=false})
                        isQuarantined=true
                        capture(clients,"pve-bulk-first-failure")
                    else
                        npc=target
                        local rangeFailure=scenario.Parallel({
                            range=function() prepareRange(clients,baselines,case,npc);return nil end,
                        })
                        if rangeFailure then
                            record({event="case_end",asset=case.asset,status="retry_required",phase="range",
                                failure=string.sub(tostring(rangeFailure),1,600),is_replayed=false})
                            local isSettledDistance=string.find(tostring(rangeFailure),
                                "close-range native placement did not settle beside target",1,true)~=nil
                            isQuarantined=not isSettledDistance
                            if not isFailureCaptured then
                                isFailureCaptured=true
                                capture(clients,"pve-bulk-first-failure")
                            end
                        else
                            local evidence
                            local castFailure=scenario.Parallel({
                                cast=function() evidence=runCast(clients,baselines,case,npc);return nil end,
                            })
                            if castFailure then
                                record({event="case_end",asset=case.asset,status="retry_required",phase="cast",
                                    failure=string.sub(tostring(castFailure),1,600),is_replayed=false})
                                local isBoundedNoDispatch=string.find(tostring(castFailure),
                                    "native input lacked exact server dispatch within bounded window",1,true)~=nil
                                local isNoEdge=string.find(tostring(castFailure),
                                    "native aim projection off screen before input; no edge submitted",1,true)~=nil
                                isQuarantined=not (isBoundedNoDispatch or isNoEdge)
                                if not isFailureCaptured then
                                    isFailureCaptured=true
                                    capture(clients,"pve-bulk-first-failure")
                                end
                            else
                                admitted=admitted+1
                                if evidence=="server_resource_effect" then effects=effects+1 end
                                record({event="case_end",asset=case.asset,status="admitted",evidence=evidence,
                                    is_packet_delivery_verified=false,is_native_rendering_verified=false})
                            end
                        end
                    end
                end
            end
        end
    end
    capture(clients,"pve-bulk-final")
    record({event="suite_end",expected=expected,admitted=admitted,resource_effects=effects,
        retry_required=expected-admitted,is_quarantined=isQuarantined,
        is_packet_delivery_verified=false,is_native_rendering_verified=false})
    if admitted~=expected then error("bulk sweep retained unverified cases; inspect retry records") end
end

