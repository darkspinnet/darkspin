-- One runtime admission/observation attempt for every previously uncovered noun.
-- Failure is a recorded outcome, never a reason to substitute another ability.
local must, check = scenario.Must, scenario.Check
local cases = {
    {ordinal = 1, noun = "necrofaves_f01_500560118951.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 2, noun = "necrofaves_f02_500561151457.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 3, noun = "necrofaves_f03_500564951406.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 4, noun = "necrofaves_f04_500564780189.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 5, noun = "necrofaves_f05_500561193878.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 6, noun = "necrofaves_f06_500562854864.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 7, noun = "necrofaves_f07_500562890182.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 8, noun = "necrofaves_f08_500561301989.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 9, noun = "necrofaves_f09_500561464167.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 10, noun = "necrofaves_f10_500561187568.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 11, noun = "necrofaves_f11_500560210153.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 12, noun = "necrofaves_f12_500560707143.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 13, noun = "necrofaves_f13_500560540776.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 14, noun = "necrofaves_f14_500560596926.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 15, noun = "necrofaves_f15_500560798636.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 16, noun = "necrofaves_f16_500560688864.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 17, noun = "necrofaves_f17_500560611709.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 18, noun = "necrofaves_f18_500560003261.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 19, noun = "necrofaves_f19_500562813162.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 20, noun = "necrofaves_f20_500559939598.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 21, noun = "top20mcspectreentry_f01_500560349705.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 22, noun = "top20mcspectreentry_f02_500561187568.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 23, noun = "top20mcspectreentry_f03_500561456309.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 24, noun = "top20mcspectreentry_f04_500562680844.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 25, noun = "top20mcspectreentry_f05_500566869057.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 26, noun = "top20mcspectreentry_f06_500563340980.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 27, noun = "top20mcspectreentry_f07_500560404449.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 28, noun = "top20mcspectreentry_f08_500561356568.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 29, noun = "top20mcspectreentry_f09_500560617532.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 30, noun = "top20mcspectreentry_f10_500560654438.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 31, noun = "top20mcspectreentry_f11_500560389013.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 32, noun = "top20mcspectreentry_f12_500561344245.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 33, noun = "top20mcspectreentry_f13_500562413177.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 34, noun = "top20mcspectreentry_f14_500560064854.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 35, noun = "top20mcspectreentry_f15_500564951406.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 36, noun = "top20mcspectreentry_f16_500561464167.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 37, noun = "top20mcspectreentry_f17_500559968418.noun", pair = 1, mode = "npc", is_source_missing = true},
    {ordinal = 38, noun = "top20mcspectreentry_f18_500560378964.noun", pair = 2, mode = "npc", is_source_missing = true},
    {ordinal = 39, noun = "top20mcspectreentry_f19_500560291084.noun", pair = 3, mode = "npc", is_source_missing = true},
    {ordinal = 40, noun = "top20mcspectreentry_f20_500566065655.noun", pair = 4, mode = "npc", is_source_missing = true},
    {ordinal = 41, noun = "dest_prefab_spectra_object_ghosttotem.noun", pair = 1, mode = "fixture", is_source_missing = false},
    {ordinal = 42, noun = "dest_prefab_spectra_object_stealthball.noun", pair = 2, mode = "fixture", is_source_missing = false},
    {ordinal = 43, noun = "dest_prefab_tnx_object_enrageplant.noun", pair = 3, mode = "fixture", is_source_missing = false},
    {ordinal = 44, noun = "dest_prefab_tnx_object_lightningball.noun", pair = 4, mode = "fixture", is_source_missing = false},
    {ordinal = 45, noun = "dest_prefab_tnx_object_petcage.noun", pair = 1, mode = "fixture", is_source_missing = false},
    {ordinal = 46, noun = "zelems_slow_aura.noun", pair = 2, mode = "fixture", is_source_missing = false},
    {ordinal = 47, noun = "sackofhitpoints.noun", pair = 1, mode = "observed", is_source_missing = false},
    {ordinal = 48, noun = "scaldronboss_stage1deathjob.noun", pair = 2, mode = "observed", is_source_missing = false},
    {ordinal = 49, noun = "scaldronboss_stage2deathjob.noun", pair = 3, mode = "observed", is_source_missing = false},
}
local pairCount = 4

local function clean(reason)
    return tostring(reason or ""):gsub("[\r\n]", " "):sub(1, 1000)
end

local function log(message)
    check(scenario.Log("npc_remaining_pass " .. message))
end

local function launch(server, pair, role)
    local noun = role == "victim" and 709733043 or 185979955
    local profile = must(server:CreateProfile({name = "remaining-" .. pair.index .. "-" .. role,
        hero_nouns = {noun, 117394854, 576185321}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "remaining-" .. pair.index .. "-" .. role,
        launch_mode = "detached"}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    log("client pair=" .. pair.index .. " role=" .. role .. " pid=" .. client.PID)
    return client, profile
end

local function prepare(pair)
    must(pair.victim.Party:Create("remaining-" .. pair.index))
    must(pair.victim.Party:Invite(pair.observer))
    must(pair.observer.Party:Join(pair.victim))
    for _, client in ipairs({pair.victim, pair.observer}) do
        must(client.World:Configure({run_seed = 104729, is_ambient_population_suppressed = true}))
        must(client.Scene:Set(scene.Navigation))
    end
    must(pair.victim.Mission:Select(1))
    must(pair.victim.Mission:Start())
    must(pair.observer.Mission:WaitDeployment())
end

local function world(client)
    local current = must(client.World:SnapshotLiving())
    if not current.is_available or not current.hero or current.hero.is_defeated
        or not current.campaign or #current.campaign.members ~= 2 then
        error("remaining pass lost its living two-player world")
    end
    return current
end

local function finalState(pair, case)
    for _, client in ipairs({pair.victim, pair.observer}) do
        local current = world(client)
        local native = must(client.World:SnapshotNative()).gameplay_state
        if not native or not native.is_available or not native.is_world_active
            or not native.frame_sequence or native.frame_sequence <= 0 then
            error("remaining pass lost native gameplay")
        end
        for _, member in ipairs(current.campaign.members) do
            for _, character in ipairs(member.characters) do
                if character.index == 0 then
                    log("final_hero ordinal=" .. case.ordinal .. " noun=" .. case.noun
                        .. " pair=" .. pair.index .. " client=" .. client.ID
                        .. " user=" .. member.user_id .. " hp=" .. character.hit_point
                        .. " frame=" .. native.frame_sequence)
                end
            end
        end
    end
end

local function attempt(pair, case)
    for _, client in ipairs({pair.victim, pair.observer}) do
        must(client.World:RestoreHero({maximum_hit_point = 1000000, hit_point = 1000000}))
        must(client.World:ResetCooldowns())
    end
    log("case_begin ordinal=" .. case.ordinal .. " noun=" .. case.noun .. " pair=" .. pair.index
        .. " mode=" .. case.mode .. " victim_pid=" .. pair.victim.PID
        .. " observer_pid=" .. pair.observer.PID)
    local actor, receipt, failure
    if case.mode == "fixture" then
        actor, failure = pair.victim.World:SpawnControlledFixture(case.noun)
    elseif case.mode == "observed" then
        receipt, failure = pair.victim.World:Spawn(case.noun)
    else
        actor, failure = pair.victim.World:SpawnControlledNPC(case.noun)
    end
    local status, reason, object = "spawn_rejected", clean(failure), 0
    if failure then
        -- Rejections can return a partial receipt alongside their error.
        -- It is not a protected NPC handle and has no control methods.
        if type(actor) == "table" then object = actor.object_id or 0 end
        actor = nil
    end
    if actor then
        receipt = actor.InitialSnapshot
        object = receipt and receipt.object_id or actor.ObjectID or 0
        -- Never start a guessed fallback for a noun whose authored AI is missing.
        status = case.is_source_missing and "spawned_missing_authored_ai" or "spawned_callbacks_unverified"
        reason = case.is_source_missing and "authored AI source missing; fallback not executed"
            or "spawn admitted; authored callback execution remains unverified"
        check(scenario.Sleep(500))
        local retired, retireError = actor:Retire()
        if not retired or not retired.is_accepted or not retired.is_settled then
            reason = reason .. "; cleanup=" .. clean(retireError or "retirement did not settle")
        end
    elseif receipt and not failure then
        object = receipt.object_id or 0
        check(scenario.Sleep(1200))
        if case.noun == "sackofhitpoints.noun" then
            status, reason = "spawned_no_authored_abilities", "dummy has no authored ability hooks"
        else
            status, reason = "spawned_owner_context_unverified", "death job needs its actual boss owner; no generic action credited"
        end
        -- These ordinary spawns have no controlled handle. Zone logout owns cleanup.
    end
    finalState(pair, case)
    log("case_end ordinal=" .. case.ordinal .. " noun=" .. case.noun .. " pair=" .. pair.index
        .. " mode=" .. case.mode .. " status=" .. status .. " object=" .. object .. " reason=" .. reason)
end

return function()
    check(scenario.RequireAPI(1))
    if #cases ~= 49 then error("remaining pass requires its exact 49-noun manifest") end
    local server = must(scenario.StartServer({}))
    local pairs, branches = {}, {}
    for index = 1, pairCount do
        local pair = {index = index}
        pairs[index] = pair
        branches["launch_" .. index] = function()
            pair.victim, pair.victimProfile = launch(server, pair, "victim")
            pair.observer, pair.observerProfile = launch(server, pair, "observer")
            return nil
        end
    end
    check(scenario.Parallel(branches))
    for _, pair in ipairs(pairs) do prepare(pair) end
    branches = {}
    for _, pair in ipairs(pairs) do
        branches["victim_" .. pair.index] = function() must(pair.victim.Mission:Continue()); return nil end
        branches["observer_" .. pair.index] = function() must(pair.observer.Mission:Continue()); return nil end
    end
    check(scenario.Parallel(branches))
    branches = {}
    for _, pair in ipairs(pairs) do
        branches["attempt_" .. pair.index] = function()
            for _, client in ipairs({pair.victim, pair.observer}) do
                must(client.Mission:Wait())
                must(client.World:Ready())
            end
            local current, peer = world(pair.victim), world(pair.observer)
            if current.game_id ~= peer.game_id then error("remaining pass pair split worlds") end
            log("pair_ready pair=" .. pair.index .. " game=" .. current.game_id)
            for _, case in ipairs(cases) do
                if case.pair == pair.index then attempt(pair, case) end
            end
            return nil
        end
    end
    local failure = scenario.Parallel(branches)
    for _, pair in ipairs(pairs) do
        for _, client in ipairs({pair.victim, pair.observer}) do check(client:Terminate()) end
        check(pair.victimProfile:Logout())
        check(pair.observerProfile:Logout())
    end
    check(failure)
    log("suite_end attempted=" .. #cases .. " pairs=" .. pairCount)
    return nil
end
