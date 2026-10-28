---Five independent owned native duels. Run with --lua-timeout 20m.
---For a short smoke run set pairCount=1 and select case IDs below. Reruns
---reuse these exact owned clients; an attempted input is never replayed here.
local pairCount = 5
local selectedCaseIDs = {}
local isFallbackSurveyEnabled = false
local isThunderstormEnabled = true -- Separate long case, explicit 30s deadline.
local must, check = scenario.Must, scenario.Check

local function pause(ms) check(scenario.Sleep(ms)) end

local function jsonString(text)
    local escaped = tostring(text):gsub('[%z\1-\31\\"]', function(character)
        return string.format("\\u%04x", string.byte(character))
    end)
    return '"' .. escaped .. '"'
end

local function json(item)
    local kind = type(item)
    if item == nil then return "null" end
    if kind == "string" then return jsonString(item) end
    if kind == "boolean" then return item and "true" or "false" end
    if kind == "number" then return tostring(item) end
    if kind ~= "table" then return jsonString(tostring(item)) end
    local keys, entries = {}, {}
    for key in pairs(item) do keys[#keys+1] = tostring(key) end
    table.sort(keys)
    for _, key in ipairs(keys) do entries[#entries+1] = jsonString(key) .. ":" .. json(item[key]) end
    return "{" .. table.concat(entries, ",") .. "}"
end

local function record(event)
    event.suite = "pvp_bulk"
    event.at_ms = scenario.Time()
    local line = json(event)
    if #line > 2048 then error("bulk record exceeds scenario.Log limit: " .. tostring(event.event)) end
    check(scenario.Log(line))
end

local plans = {
    {firstNoun=3524487169, secondNoun=185979955, cases={
        {id="srs-basic", side="a", noun=3524487169, asset="MissileTempestBasic", kind="basic"},
        {id="missile-barrage", side="a", noun=3524487169, asset="MissileTempestActive", kind="burst", x=575, timeout=15000},
        {id="meditron-basic", side="b", noun=185979955, asset="FieldMedicBasic", kind="audit", slot="basic", isAudit=true, settle=1800, timeout=7000},
        {id="reconstruct-self", side="b", noun=185979955, asset="FieldMedicSupport", kind="heal", x=640, settle=3800, isSelf=true},
    }},
    {firstNoun=577654149, secondNoun=749013658, cases={
        {id="krel-basic", side="a", noun=577654149, asset="FireRavagerBasic", kind="basic"},
        {id="electron-sphere", side="a", noun=577654149, asset="PlasmaRandom_LightningBall", kind="damage", x=605, settle=6000, isAudit=true},
        {id="sage-healing-projectile", side="b", noun=749013658, asset="SupportHealerBasic", kind="basic_heal"},
        {id="tree-of-life-self", side="b", noun=749013658, asset="TreeOfLife", kind="heal", x=575, settle=31000, isSelf=true},
    }},
    {firstNoun=700180862, secondNoun=3618328019, cases={
        {id="lumin-basic", side="a", noun=700180862, asset="LightningTempest_Basic", kind="basic"},
        {id="thunderstorm", side="a", noun=700180862, asset="LightningTempest_Active", kind="burst", x=575, timeout=30000, isSelf=true, isLong=true, expectedLaunchCount=30, isZeroDamageAllowed=true},
        {id="thunderstorm-reset-repeat", side="a", noun=700180862, asset="LightningTempest_Active", kind="burst", x=575, timeout=30000, isSelf=true, isLong=true, expectedLaunchCount=30, isCooldownRepeat=true, isZeroDamageAllowed=true},
        {id="chain-lightning-direct", side="a", noun=700180862, asset="LightningTempest_Support", kind="damage", x=640, settle=1800},
        {id="fire-wave", side="a", noun=700180862, asset="PlasmaRandom_FireWave", kind="damage", x=605, settle=6000, isAudit=true},
        {id="delaying-sphere", side="b", noun=3618328019, asset="SpacetimeRandom2", kind="aura", x=605, settle=13000, isSelf=true, isAudit=true},
    }},
    {firstNoun=171910024, secondNoun=820281267, cases={
        {id="goliath-basic", side="a", noun=171910024, asset="EnergySentinelBasic", kind="basic"},
        {id="arc-weld-direct", side="a", noun=171910024, asset="EnergySentinelActive", kind="audit", x=640, isAudit=true, settle=1800},
        {id="trapper-grenade", side="b", noun=820281267, asset="Trapper_Grenade", kind="basic"},
    }},
    {firstNoun=43291678, secondNoun=3877795735, cases={
        {id="arakna-basic", side="a", noun=43291678, asset="SoulRavagerBasic", kind="basic"},
        {id="essence-volley", side="a", noun=43291678, asset="SoulRavagerSupport", kind="burst_heal", x=640, timeout=12000},
        {id="phantom-burst", side="a", noun=43291678, asset="SoulRavagerActive", kind="burst", x=575, timeout=10000, isSelf=true},
        {id="force-pulse", side="b", noun=3877795735, asset="ForcePulse", kind="basic", isAudit=true, settle=1200},
        {id="celestial-comet", side="b", noun=3877795735, asset="SpacetimeRandom3", kind="damage", x=605, settle=1800},
    }},
}

-- Complete the rank-one shared-executor catalog in the same five owned duels.
-- These cases require exact admitted dispatch and both native frames; semantic
-- effects and packet delivery are evaluated from the retained case traces.
local auditCases = {
    {noun=117394854,asset="SplinteringCleave",slot="basic"},
    {noun=117394854,asset="CastEnrage",slot="random",isSelf=true},
    {noun=117394854,asset="ArborealMight",slot="special_one",isSelf=true},
    {noun=117394854,asset="EntanglingRush",slot="special_two",settle=3500,timeout=9000},
    {noun=171910024,asset="ClaymoreTrap",slot="random",isSelf=true,settle=5000,timeout=12000},
    {noun=820281267,asset="TurretTrap",slot="special_one",isSelf=true,settle=12000,timeout=18000,targetMaximumHitPoint=10000},
    {noun=820281267,asset="PipeBomb",slot="special_two",settle=5000,timeout=12000},
    {noun=171910024,asset="EnergySentinelSupport",slot="special_two"},
    {noun=185979955,asset="FieldMedicActive",slot="special_two",isSelf=true,settle=1500,timeout=7000},
    {noun=185979955,asset="TechRandom",slot="random"},
    {noun=239643518,asset="ShadowRavagerBasic",slot="basic"},
    {noun=43291678,asset="NecroRandom",slot="random",settle=7500,timeout=13000,targetMaximumHitPoint=10000},
    {noun=239643518,asset="CastSoulLink",slot="random",isSelf=true},
    {noun=239643518,asset="ShadowRavagerSupport",slot="special_one",isSelf=true},
    {noun=239643518,asset="ShadowRavagerActive",slot="special_two"},
    {noun=454054015,asset="LightningRogueBasic",slot="basic"},
    {noun=454054015,asset="LightningRogueActive",slot="special_two",settle=1800,timeout=7000},
    {noun=454054015,asset="PlasmaRandom",slot="random"},
    {noun=577654149,asset="FireRavagerActive",slot="special_two"},
    {noun=577654149,asset="FireRavagerSupport",slot="special_one",settle=2600,timeout=8000,targetMaximumHitPoint=10000},
    {noun=709733043,asset="FireTempestSupport",slot="special_one",settle=7000,timeout=12000,targetMaximumHitPoint=10000},
    {noun=709733043,asset="FireTempestActive",slot="special_two",isSelf=true,settle=8500,timeout=13000,targetMaximumHitPoint=10000},
    {noun=709733043,asset="FireTempestBasic",slot="basic"},
    {noun=308338531,asset="Sporogenesis",slot="special_one",isSelf=true,settle=6500,timeout=11000},
    {noun=308338531,asset="Sprout",slot="basic",settle=6500,timeout=11000,targetMaximumHitPoint=10000},
    {noun=308338531,asset="SleepingCloud",slot="special_two",settle=7500,timeout=12000,targetMaximumHitPoint=10000},
    {noun=713294352,asset="LFPoisonRavager_Stab",slot="basic"},
    {noun=713294352,asset="LFPoisonRavager_PoisonNova",slot="special_one",isSelf=true,settle=11000,timeout=16000,targetMaximumHitPoint=10000},
    {noun=713294352,asset="LFPoisonRavager_Expunge",slot="special_two",settle=2000,timeout=7500,targetMaximumHitPoint=10000,prepAsset="LFPoisonRavager_PoisonNova",prepWaitMS=1800},
    {noun=725345831,asset="SpacetimeRandom1",slot="random"},
    {noun=725345831,asset="LightspeedTempestBasic",slot="basic"},
    {noun=725345831,asset="LightspeedTempestSupport",slot="special_one"},
    {noun=725345831,asset="LightspeedTempestActive",slot="special_two",settle=2200,timeout=7500,targetMaximumHitPoint=10000},
    {noun=753829266,asset="Pummel",slot="basic"},
    {noun=753829266,asset="AfflictionBolt",slot="random",settle=10500,timeout=15000,targetMaximumHitPoint=10000},
    {noun=753829266,asset="DeathsEmbrace",slot="special_two",isSelf=true,settle=3000,timeout=8000,targetMaximumHitPoint=10000},
    {noun=753829266,asset="Ghostform",slot="special_one",isSelf=true},
    {noun=647027626,asset="SummonSprite",slot="random",isSelf=true,settle=32000,timeout=38000},
    {noun=758141691,asset="Weakness",slot="basic"},
    {noun=758141691,asset="Psistorm",slot="special_two",settle=7500,timeout=12000,targetMaximumHitPoint=10000},
    {noun=758141691,asset="Terrify",slot="special_one",settle=5500,timeout=10500,targetMaximumHitPoint=10000},
    {noun=1077715391,asset="TimeRavagerBasic",slot="basic"},
    {noun=1077715391,asset="TimeRavagerActive",slot="special_two",isSelf=true},
    {noun=1077715391,asset="TimeRavagerSupport",slot="special_one",settle=4300,timeout=8500,targetMaximumHitPoint=10000},
    {noun=1194511925,asset="TCShieldedSentinelBasic",slot="basic"},
    {noun=1194511925,asset="TCShieldedSentinelSupport",slot="special_one"},
    {noun=1194511925,asset="TCShieldedSentinelActive",slot="special_two",isSelf=true,settle=1600,timeout=7000,targetMaximumHitPoint=10000},
    {noun=1194511925,asset="TechRandom2",slot="random",isSelf=true,settle=4300,timeout=9000,reactAfterMS=900},
    {noun=1240682488,asset="QuantumStrike",slot="basic"},
    {noun=1240682488,asset="QuantumBlink",slot="special_two",settle=3500,timeout=10000,targetMaximumHitPoint=10000},
    {noun=1240682488,asset="QuantumState",slot="special_one",isSelf=true,settle=9000,timeout=14000,targetMaximumHitPoint=10000,reactAfterMS=400},
    {noun=1237326306,asset="RootingPlague",slot="random",settle=9000,timeout=14000,targetMaximumHitPoint=10000},
    {noun=1264710567,asset="BeastSmash",slot="basic"},
    {noun=1264710567,asset="SummonBeast",slot="special_one",isSelf=true,settle=5000,timeout=10000,targetMaximumHitPoint=10000},
    {noun=1264710567,asset="BeastCharge",slot="special_two",settle=3500,timeout=9000},
    {noun=1442915581,asset="VoodooTempestBasic",slot="basic",settle=2500,timeout=7500},
    {noun=1442915581,asset="VoodooTempestActive",slot="special_two",settle=5000,timeout=10000,targetMaximumHitPoint=10000},
    {noun=1442915581,asset="VoodooTempestSupport",slot="special_one",isSelf=true,settle=1600,timeout=7000},
    {noun=1557965449,asset="TechRandom3",slot="random",settle=1800,timeout=7000,targetMaximumHitPoint=10000},
    {noun=507464763,asset="SpacetimeRandom",slot="random",settle=8500,timeout=13000,targetMaximumHitPoint=10000},
    {noun=507464763,asset="RepulsionWave",slot="special_one",isSelf=true,settle=2300,timeout=7500,targetMaximumHitPoint=10000},
    {noun=507464763,asset="GravityStorm",slot="special_two",settle=7200,timeout=12000,targetMaximumHitPoint=10000},
    {noun=308338531,asset="BioRandom2",slot="random",settle=3500,timeout=9000,targetMaximumHitPoint=10000},
    {noun=454054015,asset="LightningRogueSupport",slot="special_one",settle=4300,timeout=8500,targetMaximumHitPoint=10000},
    {noun=1476711747,asset="BinarySentinelActive",slot="special_two",settle=1800,timeout=7000,targetMaximumHitPoint=10000},
    {noun=1476711747,asset="BinarySentinelSupport",slot="special_one",settle=2200,timeout=7500,targetMaximumHitPoint=10000},
    {noun=962104613,asset="PlasmaRandom_WebbedLightning",slot="random",kind="burst",timeout=12000,targetMaximumHitPoint=10000},
    {noun=576185321,asset="PlasmaSentinelBasic",slot="basic",settle=2500,timeout=8000},
    {noun=576185321,asset="PlasmaSentinelSupport",slot="special_one",settle=8500,timeout=12500},
    {noun=576185321,asset="PlasmaSentinelActive",slot="special_two",isSelf=true,settle=8500,timeout=12500,targetMaximumHitPoint=10000,reactAfterMS=900},
    {noun=1014804343,asset="PhantomCharge",slot="random",settle=3500,timeout=9000},
    {noun=1476711747,asset="BinarySentinelBasic",slot="basic"},
    {noun=749013658,asset="SupportHealerSupport",slot="special_one",settle=8500,timeout=13000,targetMaximumHitPoint=10000},
    {noun=2038499613,asset="MissileTempestSupport",slot="special_one"},
}
for index,case in ipairs(auditCases) do
    case.id="audit-"..case.asset
    case.side="a"
    case.kind=case.kind or "audit"
    case.isAudit=case.kind=="audit"
    case.x=case.slot=="random" and 605 or case.slot=="special_one" and 640 or 575
    case.settle=case.settle or 1500
    case.timeout=case.timeout or 7000
    local plan=plans[(index-1)%pairCount+1]
    plan.cases[#plan.cases+1]=case
end

-- Survey remaining build-103 assets without treating a fallback admission as
-- parity. Enable separately from the verified catalog; a rejected cast is
-- recorded and the next owned fixture can still be inspected.
local fallbackSurveyCases = {
}
for index,case in ipairs(fallbackSurveyCases) do
    case.id="survey-"..case.asset
    case.side="a"
    case.kind="survey"
    case.isSurvey=true
    case.x=case.slot=="random" and 605 or case.slot=="special_one" and 640 or 575
    case.settle=1500
    case.timeout=4500
    local plan=plans[(index-1)%pairCount+1]
    plan.cases[#plan.cases+1]=case
end

local function snapshot(client)
    local world = must(client.World:SnapshotLiving())
    if not world.arena or not world.arena.is_combat_active then error("owned active Arena unavailable") end
    return world
end

local function native(client)
    local state = must(client.World:SnapshotNative()).gameplay_state
    if not state or not state.is_world_active or not state.position then error("owned native world unavailable") end
    return state
end

local function member(world)
    for _, participant in ipairs(world.arena.members) do
        if participant.user_id == world.user_id then return participant end
    end
    error("owned user absent from Arena roster")
end

local function actorRecord(caseID, stage, role, client, world)
    local ledger = member(world)
    local hero = world.hero
    record({event="actor", case_id=caseID, stage=stage, role=role, client_id=client.ID,
        pid=client.PID, instance_id=client.InstanceID, user_id=world.user_id, game_id=world.game_id,
        peer_generation=world.peer_generation, transport_generation=world.transport_generation,
        object_id=hero.object_id, noun=hero.noun, index=hero.index, hp=hero.hit_point,
        max_hp=hero.maximum_hit_point, power=hero.mana_point, max_power=hero.maximum_mana_point,
        damage_done=ledger.damage_done, damage_taken=ledger.damage_taken,
        healing_done=ledger.healing_done, healing_received=ledger.healing_received,
        ability_revision=world.ability and world.ability.last_revision})
end

local function stop(client)
    local receipt = must(client.World:Click(native(client).position))
    if not receipt.is_processed then error("native stop input unprocessed; no replay") end
end

local function resetCooldowns(client)
    local receipt = must(client.World:ResetCooldowns())
    if not receipt.is_queued or not receipt.is_cooldown_reset or receipt.recipient_count ~= 2 then
        error("cooldown fixture was not queued to exactly both owned duel participants")
    end
end

local function sameActor(first,second)
    return first.game_id==second.game_id and first.peer_generation==second.peer_generation
        and first.transport_generation==second.transport_generation
        and first.hero.object_id==second.hero.object_id and first.hero.index==second.hero.index
end

local function waitNativeStable(client,oldHandle,noun)
    local deadline = scenario.Time()+8000
    local previous
    local stableCount = 0
    while scenario.Time()<deadline do
        pause(100)
        local probe,probeError = client.World:SnapshotNative()
        if probe and (probe.pid~=client.PID or probe.instance_id~=client.InstanceID) then
            error("native deployment observation changed its owned process identity")
        end
        local current = probe and probe.gameplay_state
        local world,worldError = client.World:SnapshotLiving()
        local isAvailable = not probeError and not worldError and probe and probe.is_processed
            and current and current.is_available and current.is_world_active and current.controlled_handle
            and current.controlled_handle~=0 and world and world.hero and world.arena and world.arena.is_combat_active
        if isAvailable and previous and current.controlled_handle==previous.controlled_handle
            and current.owner_thread_id==previous.owner_thread_id and current.frame_sequence>previous.frame_sequence
            and (oldHandle==nil or current.controlled_handle~=oldHandle) and (noun==nil or world.hero.noun==noun) then
            stableCount=stableCount+1
            if stableCount>=2 then return current,world end
        else
            stableCount=0
        end
        previous=isAvailable and current or nil
    end
    error("native replacement/deployment handle did not settle before guarded selection")
end

local function selectHeroSafely(client,slot)
    local deadline = scenario.Time()+20000
    while scenario.Time()<deadline do
        local beforeNative,before = waitNativeStable(client)
        if before.hero.index==slot then return end
        local receipt,failure = client.World:SelectHero(slot,20000)
        if not failure then
            local result = must(receipt,nil)
            if not result.is_already_selected then
                local world = snapshot(client)
                if world.hero.input_lock_remaining_ms==nil then
                    -- Compatibility for retained older binaries without the
                    -- read-only lock producer; this is a real arrival lifetime.
                    pause(1100)
                else
                    while world.hero.input_lock_remaining_ms>0 do
                        if scenario.Time()>=deadline then error("hero arrival input lock did not expire") end
                        pause(100)
                        world=snapshot(client)
                    end
                end
            end
            return result
        end
        local action = receipt and receipt.action
        local selection = action and action.hero_selection
        local actionNative = action and action.gameplay_state
        -- Only an explicit negative pre-dispatch receipt authorizes another
        -- eligibility query. Accepted, missing or ambiguous edges stop here.
        if not action or action.is_invocation_attempted~=false or action.is_invocation_accepted~=false
            or not selection or selection.requested_slot~=slot
            or selection.previous_controlled_handle~=beforeNative.controlled_handle
            or selection.previous_slot~=before.hero.index
            or not actionNative or not actionNative.is_available or not actionNative.is_world_active
            or actionNative.owner_thread_id~=beforeNative.owner_thread_id
            or actionNative.controlled_handle~=beforeNative.controlled_handle then error(failure) end
        local after,afterNative = snapshot(client),native(client)
        if not sameActor(before,after) or afterNative.controlled_handle~=beforeNative.controlled_handle
            or afterNative.owner_thread_id~=beforeNative.owner_thread_id then error(failure) end
        record({event="selection_wait",client_id=client.ID,pid=client.PID,slot=slot,
            controlled_handle=beforeNative.controlled_handle,eligibility=selection.eligibility_code,
            is_invocation_attempted=false,is_invocation_accepted=false})
        pause(100)
    end
    error("native selection eligibility remained explicitly refused before dispatch")
end

local function ensureHero(client, noun, isForced)
    local before = snapshot(client)
    if not isForced and before.hero.noun == noun and before.hero.index == 0 then return end
    if before.hero.index~=0 then
        resetCooldowns(client)
        selectHeroSafely(client,0)
    end
    if not isForced and snapshot(client).hero.noun == noun then return end
    local replacementNative = native(client)
    local receipt = must(client.World:ReplaceHero(noun))
    if not receipt.is_replacement or not receipt.is_queued or receipt.recipient_count ~= 2 then
        error("replacement was not queued to exactly both owned duel participants")
    end
    local settledNative = waitNativeStable(client,replacementNative.controlled_handle,noun)
    record({event="replacement_native_settled",client_id=client.ID,pid=client.PID,noun=noun,
        previous_handle=replacementNative.controlled_handle,controlled_handle=settledNative.controlled_handle,
        previous_frame=replacementNative.frame_sequence,frame=settledNative.frame_sequence})
    if not receipt.is_rig_ready then
        -- Compatibility for older supervisors: the repaired operation owns
        -- this ordinary native refresh and its observed rig readiness itself.
        resetCooldowns(client)
        selectHeroSafely(client,1)
        resetCooldowns(client)
        selectHeroSafely(client,0)
    end
    local finalNative = native(client)
    if finalNative.is_rig_observed~=true or finalNative.is_rig_ready~=true then
        error("replacement actor has no observed native rig; no ability input permitted")
    end
    if snapshot(client).hero.noun ~= noun then error("replacement noun was not observed after native redeployment") end
end

local function preparePairBoundary(pair)
    record({event="fixture_boundary_start",pair=pair.index,first_id=pair.first.ID,second_id=pair.second.ID})
    stop(pair.first)
    stop(pair.second)
    -- Replacement retires owned effects and fences old producer callbacks.
    -- Ordinary native away/back swaps refresh both retained deployment views.
    ensureHero(pair.first,pair.plan.firstNoun,true)
    ensureHero(pair.second,pair.plan.secondNoun,true)
    record({event="fixture_boundary_complete",pair=pair.index,game_id=snapshot(pair.first).game_id,
        first_noun=snapshot(pair.first).hero.noun,second_noun=snapshot(pair.second).hero.noun,
        is_packet_proven=false})
end

local function restore(client, isWounded, maximumHitPoint)
    local before = snapshot(client)
    local req = {}
    if maximumHitPoint then req.maximum_hit_point = maximumHitPoint end
    if isWounded then req.hit_point = (maximumHitPoint or before.hero.maximum_hit_point) * 0.4 end
    local receipt = must(client.World:RestoreHero(req))
    if not receipt.is_queued or receipt.recipient_count ~= 2 then error("resource fixture not queued to both owned clients") end
    local expectedMaximum = maximumHitPoint or before.hero.maximum_hit_point
    local expectedHP = isWounded and expectedMaximum*0.4 or expectedMaximum
    local deadline = scenario.Time()+4000
    while scenario.Time()<deadline do
        local after = snapshot(client)
        if math.abs(after.hero.hit_point-expectedHP)<0.1 and math.abs(after.hero.maximum_hit_point-expectedMaximum)<0.1
            and math.abs(after.hero.mana_point-after.hero.maximum_mana_point)<0.1 then return end
        pause(100)
    end
    error("server resource fixture was not observed before deadline")
end

local function place(client, point)
    local before = native(client)
    local receipt = must(client.World:PlaceHero(point))
    if not receipt.is_queued or receipt.recipient_count ~= 2 then error("placement not queued to both owned clients") end
    local deadline = scenario.Time()+8000
    while scenario.Time()<deadline do
        local after = native(client)
        local dx,dy = after.position.x-point.x,after.position.y-point.y
        -- Some build-103 controlled rigs settle with a small persistent actor-center offset.
        if dx*dx+dy*dy<0.25 and after.frame_sequence>before.frame_sequence then return end
        pause(100)
    end
    error("fresh native placement not observed")
end

local function isPinned(before, after)
    return before.game_id==after.game_id and before.peer_generation==after.peer_generation
        and before.transport_generation==after.transport_generation
        and before.hero.object_id==after.hero.object_id and before.hero.index==after.hero.index
end

local function nativeDispatch(case, source, target)
    local aim = native(case.isSelf and source or target).position
    if not case.isSelf then aim={x=aim.x,y=aim.y,z=aim.z+1.5} end
    if case.kind=="basic" or case.kind=="basic_heal" or (case.isAudit or case.isSurvey) and case.slot=="basic" then
        local receipt = must(source.World:Hold(aim,600))
        local click = receipt.gameplay_click
        if not receipt.is_processed or not click or not click.is_press_poll_observed or not click.is_release_poll_observed then
            error("basic input lacks paired native press/release processing; no replay")
        end
        return
    end
    local button = must(source.UI:Click(case.x,655))
    if not button.is_processed then error("native hotbar edge unprocessed; no replay") end
    pause(150)
    local receipt = must(source.World:Click(aim))
    if not receipt.is_processed then error("native world ability edge unprocessed; no replay") end
end

local function waitCase(pair, case, source, target, beforeSource, beforeTarget, sourceFrame, targetFrame)
    local started = scenario.Time()
    local deadline = started+(case.timeout or math.max(5000,(case.settle or 0)+3000))
    local isBurst = case.kind=="burst" or case.kind=="burst_heal"
    local previousRevision = beforeSource.ability and beforeSource.ability.last_revision
    if isBurst and previousRevision==nil then error("burst terminal revision producer unavailable") end
    local afterSource,afterTarget
    local otherDispatchRevision = 0
    while scenario.Time()<deadline do
        afterSource,afterTarget = snapshot(source),snapshot(target)
        if not isPinned(beforeSource,afterSource) or not isPinned(beforeTarget,afterTarget) then
            error("cast changed a pinned actor or realm; retain partial result without replay")
        end
        local sourceLedger,targetLedger = member(afterSource),member(afterTarget)
        local damage = targetLedger.damage_taken-member(beforeTarget).damage_taken
        local healing = sourceLedger.healing_received-member(beforeSource).healing_received
        local isDamage = damage>0 and afterTarget.hero.hit_point<beforeTarget.hero.hit_point
        local isHealing = healing>0 and afterSource.hero.hit_point>beforeSource.hero.hit_point
        if case.isSurvey then
            local dispatch=afterSource.ability and afterSource.ability.last_dispatch
            local previous=beforeSource.ability and beforeSource.ability.last_dispatch
            local previousDispatchRevision=previous and previous.revision or 0
            if dispatch and dispatch.revision>previousDispatchRevision then
                if dispatch.actor_object_id~=beforeSource.hero.object_id or dispatch.ability_asset~=case.asset then
                    if dispatch.revision>otherDispatchRevision then
                        record({event="survey_other_dispatch",case_id=case.id,asset=case.asset,
                            observed_asset=dispatch.ability_asset,revision=dispatch.revision})
                        otherDispatchRevision=dispatch.revision
                    end
                else
                    local sourceNative,targetNative=native(source),native(target)
                    if sourceNative.frame_sequence>sourceFrame and targetNative.frame_sequence>targetFrame
                        and scenario.Time()-started>=case.settle then
                        record({event="survey_observation",case_id=case.id,asset=case.asset,
                            is_dispatch_observed=true,revision=dispatch.revision,damage=damage,healing=healing,
                            source_frame=sourceNative.frame_sequence,target_frame=targetNative.frame_sequence,
                            is_effect_proven=false,is_packet_proven=false})
                        return afterSource,afterTarget,"survey_admitted_unverified_effect"
                    end
                end
            end
        elseif case.isAudit then
            local dispatch=afterSource.ability and afterSource.ability.last_dispatch
            local previous=beforeSource.ability and beforeSource.ability.last_dispatch
            local previousDispatchRevision=previous and previous.revision or 0
            if dispatch and dispatch.revision>previousDispatchRevision then
                if dispatch.actor_object_id~=beforeSource.hero.object_id or dispatch.ability_asset~=case.asset then
                    error("audit dispatch identity disagrees with native case")
                end
                local sourceNative,targetNative=native(source),native(target)
                if sourceNative.frame_sequence>sourceFrame and targetNative.frame_sequence>targetFrame
                    and scenario.Time()-started>=case.settle then
                    record({event="audit_dispatch",case_id=case.id,asset=case.asset,
                        revision=dispatch.revision,source_frame=sourceNative.frame_sequence,
                        target_frame=targetNative.frame_sequence,damage=damage,healing=healing,
                        is_packet_proven=false,is_native_rendering_proven=false})
                    return afterSource,afterTarget,"exact_dispatch_and_native_frames"
                end
            end
        elseif isBurst then
            local ability = afterSource.ability
            if not ability then error("burst terminal observer unavailable") end
            if ability.last_revision>previousRevision+1 then error("burst revision skipped; no attribution or replay") end
            local cast = ability.cast
            if ability.last_revision==previousRevision+1 and cast then
                if cast.actor_object_id~=beforeSource.hero.object_id or cast.ability_asset~=case.asset then error("burst terminal identity disagrees with dispatched case") end
                if cast.is_failed then error("burst producer failed: "..tostring(cast.failure_reason)) end
                if cast.is_accepted and cast.is_released and cast.is_burst_completed
                    and cast.expected_launch_count>0 and cast.launch_count==cast.expected_launch_count then
                    pair.isCaseTerminal=true
                    record({event="burst_terminal",case_id=case.id,asset=cast.ability_asset,
                        revision=cast.revision,launch_count=cast.launch_count,expected_launch_count=cast.expected_launch_count,
                        accepted_at=cast.accepted_at,burst_completed_at=cast.burst_completed_at,
                        is_released=cast.is_released,is_burst_completed=cast.is_burst_completed,
                        damage=damage,healing=healing})
                    if case.expectedLaunchCount and cast.expected_launch_count~=case.expectedLaunchCount then
                        error("terminal burst launch count disagrees with the explicit installed case contract")
                    end
                    if not isDamage and (case.asset=="PlasmaRandom_WebbedLightning" or case.isZeroDamageAllowed) then
                        record({event="burst_zero_damage",case_id=case.id,asset=case.asset,
                            is_defense_packet_verified=false,is_effect_proven=false,is_packet_proven=false})
                        return afterSource,afterTarget,"terminal_zero_damage_unverified"
                    end
                    if not isDamage then error("burst completed but damage remains unverified") end
                    if case.kind=="burst_heal" and not isHealing then error("burst completed but actual healing remains unverified") end
                    return afterSource,afterTarget,"terminal_and_resources"
                end
            end
        elseif case.kind=="aura" then
            -- Duration is deliberately respected. This is admission/resource
            -- evidence only; this snapshot API cannot certify enemy slow expiry.
            if scenario.Time()-started>=case.settle then
                if afterSource.hero.mana_point>=beforeSource.hero.mana_point then error("aura power consumption unverified") end
                return afterSource,afterTarget,"resource_only_unverified_effect"
            end
        elseif scenario.Time()-started >= (case.settle or 1800) then
            if case.kind=="heal" and isHealing then return afterSource,afterTarget,"healing_ledger" end
            if case.kind=="basic_heal" and isDamage and isHealing then return afterSource,afterTarget,"damage_and_healing_ledgers" end
            if case.kind~="heal" and case.kind~="basic_heal" and isDamage then return afterSource,afterTarget,"damage_ledger" end
        end
        pause(150)
    end
    actorRecord(case.id,"deadline","source",source,afterSource or beforeSource)
    actorRecord(case.id,"deadline","target",target,afterTarget or beforeTarget)
    if case.isSurvey then
        record({event="survey_observation",case_id=case.id,asset=case.asset,
            is_dispatch_observed=false,is_effect_proven=false,is_packet_proven=false})
        return afterSource or beforeSource,afterTarget or beforeTarget,"survey_no_dispatch_observer"
    end
    error("case observation deadline reached; no replay")
end

local meleeAssets = {
    SplinteringCleave=true, EnergySentinelActive=true, EnergySentinelSupport=true, ShadowRavagerBasic=true, ShadowRavagerActive=true,
    LightningRogueBasic=true, LightningTempest_Support=true,
    LFPoisonRavager_Stab=true, LFPoisonRavager_Expunge=true, Pummel=true, TimeRavagerBasic=true,
    QuantumStrike=true, BeastSmash=true, BinarySentinelBasic=true, TechRandom3=true, PlasmaSentinelBasic=true,
}

local function runCase(pair, case)
    pair.isCaseTerminal=false
    local source = case.side=="a" and pair.first or pair.second
    local target = case.side=="a" and pair.second or pair.first
    record({event="case_start",case_id=case.id,pair=pair.index,asset=case.asset,noun=case.noun,
        source_id=source.ID,source_pid=source.PID,target_id=target.ID,target_pid=target.PID})
    ensureHero(source,case.noun)
    restore(source,case.kind=="heal" or case.kind=="basic_heal" or case.kind=="burst_heal")
    restore(target,false,case.targetMaximumHitPoint or (case.isAudit and 10000))
    resetCooldowns(source)
    resetCooldowns(target)
    local anchor = native(target).position
    place(source,{x=anchor.x-(meleeAssets[case.asset] and 1 or 3),y=anchor.y,z=anchor.z})
    if case.prepAsset then
        nativeDispatch({isSelf=true,kind="audit",slot="special_one",x=640},source,target)
        pause(case.prepWaitMS or 1500)
        record({event="prep_cast_processed",case_id=case.id,asset=case.prepAsset,
            source_id=source.ID,target_id=target.ID,source_pid=source.PID,target_pid=target.PID})
        resetCooldowns(source)
    end
    local beforeSource,beforeTarget = snapshot(source),snapshot(target)
    local sourceNative,targetNative = native(source),native(target)
    if not sourceNative.is_rig_observed or not sourceNative.is_rig_ready
        or not targetNative.is_rig_observed or not targetNative.is_rig_ready then
        error("case requires observed rigs on both native controlled actors before input")
    end
    record({event="native_rig_ready",case_id=case.id,source_id=source.ID,source_pid=source.PID,
        target_id=target.ID,target_pid=target.PID,source_handle=sourceNative.controlled_handle,
        target_handle=targetNative.controlled_handle,source_frame=sourceNative.frame_sequence,
        target_frame=targetNative.frame_sequence,is_source_rig_ready=true,is_target_rig_ready=true})
    actorRecord(case.id,"before","source",source,beforeSource)
    actorRecord(case.id,"before","target",target,beforeTarget)
    if (case.kind=="burst" or case.kind=="burst_heal")
        and (not beforeSource.ability or beforeSource.ability.last_revision==nil) then
        error("burst terminal revision producer unavailable before input")
    end
    if case.isCooldownRepeat and not pair.thunderstormTerminal then
        error("cooldown repeat requires the first Thunderstorm terminal result; no input attempted")
    end
    local dispatchedAt = scenario.Time()
    nativeDispatch(case,source,target)
    if case.reactAfterMS then
        pause(case.reactAfterMS)
        local aim=native(source).position
        local attack=must(target.World:Hold({x=aim.x,y=aim.y,z=aim.z+1.5},600))
        local click=attack.gameplay_click
        if not attack.is_processed or not click or not click.is_press_poll_observed or not click.is_release_poll_observed then
            error("reactive fixture attack lacks paired native press/release processing")
        end
        record({event="reactive_attack_processed",case_id=case.id,source_id=target.ID,
            target_id=source.ID,source_pid=target.PID,target_pid=source.PID})
    end
    local nativeProcessedAt = scenario.Time()
    record({event="native_processed",case_id=case.id,elapsed_ms=scenario.Time()-dispatchedAt,
        native_processed_at_ms=nativeProcessedAt,
        is_packet_proven=false,is_ability_identity_native_verified=false})
    local afterSource,afterTarget,evidence = waitCase(pair,case,source,target,beforeSource,beforeTarget,
        sourceNative.frame_sequence,targetNative.frame_sequence)
    if case.id=="thunderstorm" then
        pair.thunderstormTerminal={nativeProcessedAt=nativeProcessedAt,cast=afterSource.ability.cast,
            objectID=beforeSource.hero.object_id,gameID=beforeSource.game_id}
    elseif case.isCooldownRepeat then
        local first,second = pair.thunderstormTerminal,afterSource.ability.cast
        local interval = nativeProcessedAt-first.nativeProcessedAt
        record({event="cooldown_reset_repeat",case_id=case.id,first_case_id="thunderstorm",
            source_id=source.ID,game_id=beforeSource.game_id,object_id=beforeSource.hero.object_id,
            first_accepted_at=first.cast.accepted_at,second_accepted_at=second.accepted_at,
            first_revision=first.cast.revision,second_revision=second.revision,
            first_launch_count=first.cast.launch_count,second_launch_count=second.launch_count,
            native_processed_interval_ms=interval,authored_cooldown_ms=30000,
            is_below_authored_cooldown=interval<30000,is_packet_proven=false})
        if first.objectID~=beforeSource.hero.object_id or first.gameID~=beforeSource.game_id or interval>=30000 then
            error("repeat completed but same-actor admission before the authored cooldown remains unverified")
        end
    end
    actorRecord(case.id,"after","source",source,afterSource)
    actorRecord(case.id,"after","target",target,afterTarget)
    return evidence
end

local function selected(case)
    if isFallbackSurveyEnabled then return case.isSurvey==true end
    if case.isSurvey then return false end
    if case.isLong and not isThunderstormEnabled then return false end
    return next(selectedCaseIDs)==nil or selectedCaseIDs[case.id]==true
end

local function validatePair(pair)
    local firstBefore,secondBefore = native(pair.first),native(pair.second)
    local deadline = scenario.Time()+8000
    while scenario.Time()<deadline do
        local firstNative,secondNative = native(pair.first),native(pair.second)
        if firstNative.frame_sequence>firstBefore.frame_sequence and secondNative.frame_sequence>secondBefore.frame_sequence then
            local first,second = snapshot(pair.first),snapshot(pair.second)
            if first.game_id~=second.game_id or #first.arena.members~=2 or #second.arena.members~=2 then
                error("final reciprocal Arena identity unavailable")
            end
            local firstMembers,secondMembers = {},{}
            for _,participant in ipairs(first.arena.members) do firstMembers[participant.user_id]=participant.team end
            for _,participant in ipairs(second.arena.members) do secondMembers[participant.user_id]=participant.team end
            if firstMembers[first.user_id]==nil or firstMembers[second.user_id]==nil
                or firstMembers[first.user_id]==firstMembers[second.user_id]
                or firstMembers[first.user_id]~=secondMembers[first.user_id]
                or firstMembers[second.user_id]~=secondMembers[second.user_id] then
                error("final reciprocal Arena roster disagrees")
            end
            actorRecord("pair-final","after","first",pair.first,first)
            actorRecord("pair-final","after","second",pair.second,second)
            record({event="pair_liveness",pair=pair.index,game_id=first.game_id,
                first_frame_before=firstBefore.frame_sequence,first_frame_after=firstNative.frame_sequence,
                second_frame_before=secondBefore.frame_sequence,second_frame_after=secondNative.frame_sequence,
                is_reciprocal_arena_observed=true,is_native_rendering_proven=false})
            -- Two success samples per duel; failure captures remain separately bounded.
            for _,client in ipairs({pair.first,pair.second}) do
                local screenshot = must(client.UI:Screenshot("bulk-pair-"..pair.index.."-final"))
                record({event="pair_screenshot",pair=pair.index,client_id=client.ID,pid=client.PID,
                    image_path=screenshot.image_path,is_available=screenshot.is_available,is_complete=screenshot.is_complete})
            end
            return
        end
        pause(100)
    end
    error("final independent native frames did not advance")
end

local function runWaveCase(job)
    local pair = job.pair
    if not pair.isWindowValidated then
        local window = must(pair.first.UI:InspectWindow()).window
        local otherWindow = must(pair.second.UI:InspectWindow()).window
        if not window or not otherWindow or window.width~=1280 or window.height~=720
            or otherWindow.width~=1280 or otherWindow.height~=720 then error("bulk native coordinates require both owned1280x720 windows") end
        pair.isWindowValidated=true
    end
    job.evidence=runCase(pair,job.case)
    job.finishedAt=scenario.Time()
    job.isComplete=true
end

local function waveFailure(failure,name)
    local cause = failure
    while cause do
        if cause.Failures and cause.Failures[name] then return tostring(cause.Failures[name]) end
        cause=cause.Cause
    end
    return failure and tostring(failure) or "branch returned no case result"
end

local function finishWaveCase(job,failure)
    local pair,case = job.pair,job.case
    local evidence = job.evidence or failure
    local status = job.isComplete and (case.isSurvey and "surveyed" or "passed") or "retry_required"
    local reportedAt = scenario.Time()
    record({event="case_end",case_id=case.id,pair=pair.index,status=status,
        elapsed_ms=(job.finishedAt or reportedAt)-job.started,
        barrier_wait_ms=job.finishedAt and reportedAt-job.finishedAt or nil,
        is_elapsed_upper_bound=job.finishedAt==nil,evidence=job.isComplete and evidence or nil,
        failure=not job.isComplete and string.sub(tostring(evidence),1,700) or nil,is_packet_proven=false})
    pair.results[#pair.results+1]={id=case.id,status=status}
    if job.isComplete then return end
    if not pair.isCaseTerminal then pair.isQuarantined=true end
    record({event="retry",case_id=case.id,pair=pair.index,asset=case.asset,noun=case.noun,
        source_id=case.side=="a" and pair.first.ID or pair.second.ID,
        reason=string.sub(tostring(evidence),1,700),is_replayed=false})
    if pair.isFailureCaptured then return end
    pair.isFailureCaptured=true
    local capture,captureError = pair.first.Diagnostics:Capture("bulk-pair-"..pair.index.."-first-failure")
    record({event="diagnostic",pair=pair.index,case_id=case.id,
        manifest=capture and capture.manifest_path,failure=captureError and string.sub(tostring(captureError),1,500)})
end

local function runWaves(pairs)
    local waveCount = 0
    for _,pair in ipairs(pairs) do waveCount=math.max(waveCount,#pair.plan.cases) end
    for wave=1,waveCount do
        local jobs,branches = {},{}
        for _,pair in ipairs(pairs) do
            local case = pair.plan.cases[wave]
            if case and selected(case) then
                local job = {pair=pair,case=case,started=scenario.Time(),name="duel_"..pair.index}
                jobs[#jobs+1]=job
                if not pair.isQuarantined then
                    branches[job.name]=function() runWaveCase(job); return nil end
                else
                    job.finishedAt=job.started
                end
            end
        end
        -- Gopher-lua pcall cannot preserve a yielding continuation. Parallel's
        -- native branch error boundary drains this wave before results/logging.
        local failure
        if next(branches) then failure=scenario.Parallel(branches) end
        for _,job in ipairs(jobs) do
            local reason = job.pair.isQuarantined and "pair quarantined after uncertain operation; no dependent input attempted"
                or waveFailure(failure,job.name)
            finishWaveCase(job,reason)
        end
    end
end

local function launch(server,id,noun)
    local client,lookupError = scenario.GetClient(id)
    if client then
        record({event="client_reused",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID})
        return client,false
    end
    local lookupCause = lookupError
    while lookupCause and (lookupCause.Code==nil or lookupCause.Code=="") do lookupCause=lookupCause.Cause end
    if lookupError and (not lookupCause or lookupCause.Code~="not_found") then error(lookupError) end
    -- Stable starter squad forces every planned family through replacement
    -- and ordinary native away/back redeployment during its first case.
    local profile = must(server:CreateProfile({name=id,hero_nouns={117394854,3068979135,576185321},
        chain_progression=2,is_pvp_enabled=true}))
    client = must(profile:Launch({client_id=id,launch_mode="detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    record({event="client_launched",client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        initial_noun=117394854,planned_noun=noun})
    return client,true
end

local function launchPair(server,pair)
    pair.first,pair.isFirstNew = launch(server,"bulk-"..pair.index.."a",pair.plan.firstNoun)
    pair.second,pair.isSecondNew = launch(server,"bulk-"..pair.index.."b",pair.plan.secondNoun)
end

local function prepareParty(pair)
    local firstWorld,firstWorldError = pair.first.World:Snapshot()
    local secondWorld,secondWorldError = pair.second.World:Snapshot()
    if firstWorld and secondWorld and firstWorld.arena and secondWorld.arena
        and firstWorld.arena.is_combat_active and secondWorld.arena.is_combat_active
        and firstWorld.game_id==secondWorld.game_id then
        pair.isDuelReused=true
        record({event="duel_reused",pair=pair.index,game_id=firstWorld.game_id})
        return
    end
    -- These fresh clients need one owned party and one match each. Never join
    -- an arbitrary existing game or replay a partially delivered PvP setup.
    if not pair.isFirstNew or not pair.isSecondNew
        or (firstWorld and (firstWorld.arena or firstWorld.game_id~=0))
        or (secondWorld and (secondWorld.arena or secondWorld.game_id~=0)) then
        error("partial or retired bulk duel requires explicit recovery; no setup replay")
    end
    must(pair.first.Party:Create("bulk-"..pair.index))
    must(pair.first.Party:Invite(pair.second))
    must(pair.second.Party:Join(pair.first))
end

local function enterPair(server,pair)
    if pair.isDuelReused then return end
    must(pair.first.Scene:Set(scene.Navigation))
    must(pair.second.Scene:Set(scene.Navigation))
    must(pair.first:WaitForReportedLocation("navigation",15000))
    must(pair.second:WaitForReportedLocation("navigation",15000))
    local match = must(server:StartPVP({teams={{pair.first},{pair.second}}}))
    -- Each Accept waits for this user's receipt only. A pair branch can admit
    -- its two clients in order without nesting another Parallel block.
    must(pair.first.PVP:Accept())
    must(pair.second.PVP:Accept())
    must(pair.first.PVP:Wait())
    must(pair.second.PVP:Wait())
    record({event="duel_started",pair=pair.index,game_id=match.game_id,
        first_id=pair.first.ID,first_pid=pair.first.PID,second_id=pair.second.ID,second_pid=pair.second.PID})
end

return function()
    check(scenario.RequireAPI(1))
    if pairCount<1 or pairCount>5 then error("bulk pairCount must be1 through5") end
    local expectedCount = 0
    for index=1,pairCount do
        for _,case in ipairs(plans[index].cases) do
            if selected(case) then
                expectedCount=expectedCount+1
                record({event="case_manifest",case_id=case.id,pair=index,side=case.side,
                    asset=case.asset,noun=case.noun,kind=case.kind,is_long=case.isLong==true,
                    timeout_ms=case.timeout,settle_ms=case.settle,
                    is_packet_proven=false,is_multi_enemy_hop_coverage=false})
            end
        end
    end
    if expectedCount==0 then error("bulk selection contains no cases in the requested pair range") end
    local server = must(scenario.StartServer({}))
    local pairs = {}
    local launchBranches = {}
    for index=1,pairCount do
        local pair = {index=index,plan=plans[index],results={}}
        pairs[index]=pair
        launchBranches["launch_"..index]=function() launchPair(server,pair); return nil end
    end
    record({event="startup_phase",phase="launch_and_login",pair_count=pairCount})
    check(scenario.Parallel(launchBranches))
    record({event="startup_phase",phase="party_fixtures",pair_count=pairCount})
    -- Party mutations are deliberately outside Parallel's independent allowlist.
    for _,pair in ipairs(pairs) do prepareParty(pair) end
    local entryBranches = {}
    for index,pair in ipairs(pairs) do
        entryBranches["entry_"..index]=function() enterPair(server,pair); return nil end
    end
    record({event="startup_phase",phase="arena_entry",pair_count=pairCount})
    check(scenario.Parallel(entryBranches))
    local gameIDs = {}
    for _,pair in ipairs(pairs) do
        local world = snapshot(pair.first)
        if gameIDs[world.game_id] then error("bulk duels share a game identity") end
        gameIDs[world.game_id]=true
        if #world.arena.members~=2 then error("bulk duel does not contain exactly two participants") end
    end
    record({event="combat_barrier",pair_count=#pairs,client_count=2*#pairs,
        is_native_rendering_proven=false,is_packet_proven=false})
    local fixtureBranches = {}
    for index,pair in ipairs(pairs) do
        fixtureBranches["fixture_"..index]=function() preparePairBoundary(pair); return nil end
    end
    check(scenario.Parallel(fixtureBranches))
    runWaves(pairs)
    local branches = {}
    for index,pair in ipairs(pairs) do
        branches["validate_"..index]=function() validatePair(pair); return nil end
    end
    local parallelError = scenario.Parallel(branches)
    local passed,surveyed,retries = 0,0,0
    for _,pair in ipairs(pairs) do
        local completedIDs = {}
        for _,result in ipairs(pair.results) do
            completedIDs[result.id]=true
            if result.status=="passed" then
                passed=passed+1
            elseif result.status=="surveyed" then
                surveyed=surveyed+1
            else
                retries=retries+1
            end
        end
        for _,case in ipairs(pair.plan.cases) do
            if selected(case) and not completedIDs[case.id] then
                retries=retries+1
                record({event="retry",case_id=case.id,pair=pair.index,asset=case.asset,noun=case.noun,
                    reason="branch ended before case result; no dependent input replayed",is_replayed=false})
            end
        end
    end
    record({event="suite_end",expected_count=expectedCount,passed=passed,surveyed=surveyed,retry_count=retries,
        branch_failure=parallelError and string.sub(tostring(parallelError),1,700),
        is_complete=parallelError==nil and retries==0,is_packet_proven=false})
    check(parallelError)
    if retries>0 then error("bulk suite retained "..retries.." failed/unverified cases in JSON retry records; no automatic replay") end
end



