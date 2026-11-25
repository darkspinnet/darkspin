---Native Darkspin command regression: ship, diagnostics and nonterminal campaign.
---Run with --lua-timeout 20m. Also run commands_terminal.lua and commands_social.lua.
---No command is dispatched through a server/debug shortcut. Fixtures are separate.
local must = scenario.Must
local check = scenario.Check

local function fail(client, label, message)
    local report, captureError = client.Diagnostics:Capture(label)
    error(message .. "; evidence=" .. tostring(report and report.manifest_path or captureError))
end

local function requireThat(client, condition, label, message)
    if not condition then fail(client, label, message) end
end

local function send(client, text, expected)
    local before = must(client.Chat:Snapshot())
    local opened = must(client.UI:Press("Enter"))
    requireThat(client, opened.is_processed, "commands-open", "chat opening input was not processed")
    check(scenario.Sleep(250)) -- Input allowance only, never acceptance evidence.
    local typed = must(client.UI:Type(text))
    requireThat(client, typed.processed_character_count == #text,
        "commands-type", "partial chat input; command will not be retried: " .. text)
    local submitted = must(client.UI:Press("Enter"))
    requireThat(client, submitted.is_processed, "commands-submit", "chat submission was not processed")
    local verb = string.lower(string.match(text, "^(/[%a]+)"))
    local receipt, replyError = client.Chat:Wait(before.cursor, verb, 30000)
    if replyError ~= nil or receipt == nil then
        fail(client, "commands-no-reply", text .. " had no fresh server reply: " .. tostring(replyError))
    end
    requireThat(client, receipt.command == verb and receipt.source == "server_command_reply"
        and receipt.is_native_rendered == false, "commands-reply-source", "incorrect command reply provenance")
    requireThat(client, expected == nil or string.find(receipt.reply, expected, 1, true) ~= nil,
        "commands-reply", text .. " returned unexpected reply: " .. receipt.reply)
    check(scenario.Log("REPLY " .. text .. " => " .. receipt.reply))
    return receipt, before.cursor
end

local function world(client)
    local snapshot = must(client.World:Snapshot())
    requireThat(client, snapshot.is_available and snapshot.hero ~= nil,
        "commands-world", "owned committed world is unavailable")
    return snapshot
end

local function waitWorld(client, initial, label, predicate)
    local deadline = scenario.Time() + 15000
    repeat
        local current = world(client)
        requireThat(client, current.game_id == initial.game_id and current.zone_generation == initial.zone_generation,
            "commands-epoch", "command changed the world unexpectedly: " .. label)
        if predicate(current) then
            check(scenario.Log("PASS effect " .. label))
            return current
        end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    fail(client, "commands-effect", "accepted command did not produce its effect: " .. label)
end

local function profileEffect(client, profile, label, predicate)
    local deadline = scenario.Time() + 10000
    repeat
        local current = must(profile:Snapshot())
        requireThat(client, current.is_resources_available, "commands-profile-observer", "owned profile resource observation unavailable")
        if predicate(current) then check(scenario.Log("PASS profile " .. label)); return current end
        check(scenario.Sleep(100))
    until scenario.Time() >= deadline
    fail(client, "commands-profile", "profile mutation was not independently observed: " .. label)
end

local function freshArtifact(client, before, label, snapshotID)
    local oldPaths = {}
    for _, artifact in ipairs(before.files) do oldPaths[artifact.path] = true end
    local deadline = scenario.Time() + 60000
    repeat
        local current = must(client.Chat:Artifacts())
        for _, artifact in ipairs(current.files) do
            local isWanted = snapshotID ~= nil
                and string.find(artifact.path, snapshotID, 1, true) ~= nil and artifact.name == "report.md"
                or snapshotID == nil and string.match(string.lower(artifact.name), "%.zip$") ~= nil
            if not oldPaths[artifact.path] and artifact.byte_size > 0 and isWanted then
                check(scenario.Log("PASS archive " .. label .. " path=" .. artifact.path))
                return artifact
            end
        end
        check(scenario.Sleep(250))
    until scenario.Time() >= deadline
    fail(client, "commands-artifact", "no fresh nonempty owned diagnostic artifact after " .. label)
end

local function diagnostics(client)
    send(client, "/ss", "Sync Snapshot mode=")
    send(client, "/ss status", "Sync Snapshot mode=")
    send(client, "/ss mode off", "mode set to off")
    send(client, "/ss dump", "Sync Snapshot is off")
    send(client, "/ss mode manual", "mode set to manual")
    send(client, "/ss delay 2", "delay set to 2s")
    send(client, "/ss delay 0", "Syntax:")
    send(client, "/ss mode invalid", "Syntax:")
    send(client, "/ss invalid", "Sync Snapshot commands:")
    local before = must(client.Chat:Artifacts())
    local receipt = send(client, "/ss dump", "Snapshot SS-")
    local snapshotID = string.match(receipt.reply, "Snapshot (SS%-%d+)")
    requireThat(client, snapshotID ~= nil, "commands-snapshot-id", "dump reply omitted snapshot identity")
    freshArtifact(client, before, "/ss dump", snapshotID)
    -- A manually created snapshot has no automatic fingerprint to suppress.
    send(client, "/ss ignore " .. snapshotID, "not created automatically")
    send(client, "/ss ignore SS-999999", "not created automatically")
    send(client, "/ss mode auto", "mode set to auto")
    send(client, "/ss status", "mode=auto")
    send(client, "/ss mode manual", "mode set to manual")
    for _, verb in ipairs({"/bug", "/b"}) do
        before = must(client.Chat:Artifacts())
        send(client, verb .. " scenario-command-regression", "Bug report queued as ")
        freshArtifact(client, before, verb)
    end
    send(client, "/bug", "Syntax:")
end

local function ship(client, profile)
    send(client, "/help", "Darkspin:")
    send(client, "/emote", "Mod emotes:")
    send(client, "/emote list", "Mod emotes:")
    -- Ordinary build-103 heroes are intentionally ineligible for Mod emotes.
    for _, emote in ipairs({"happy", "laugh", "scared", "swoon", "roar", "backflip", "flex", "yawn"}) do
        send(client, "/emote " .. emote, "Mod emotes:")
    end
    send(client, "/ping", "Pong |")
    send(client, "/PiNg", "Pong |")
    send(client, "/hint", "active deployment")
    send(client, "/loc", "active deployment")
    send(client, "/stat", "Stats only work")
    send(client, "/follow", "active multiplayer game")
    send(client, "/ai", "multiplayer co-op or PvP")
    send(client, "/god", "active mission")
    send(client, "/scale 1", "active mission")
    send(client, "/spawn CitadelBasicGunner.Noun", "Spawn requires")
    send(client, "/warp", "Warp areas:")
    send(client, "/warp not-a-real-map", "Warp areas:")
    send(client, "/noreplay off", "No replay OFF")
    profileEffect(client, profile, "/noreplay off", function(state) return not state.is_noreplay_enabled end)
    send(client, "/noreplay", "No replay ON")
    profileEffect(client, profile, "/noreplay toggle on", function(state) return state.is_noreplay_enabled end)
    send(client, "/noreplay", "No replay OFF")
    profileEffect(client, profile, "/noreplay toggle off", function(state) return not state.is_noreplay_enabled end)
    send(client, "/noreplay on", "No replay ON")
    send(client, "/noreplay invalid", "Syntax:")
    profileEffect(client, profile, "/noreplay invalid preserves state", function(state) return state.is_noreplay_enabled end)
    send(client, "/level 10", "Account level set to 10")
    profileEffect(client, profile, "/level", function(state) return state.level == 10 end)
    send(client, "/level 101", "Syntax:")
    local before = must(profile:Snapshot())
    requireThat(client, before.is_resources_available, "commands-dna-observer", "owned DNA observation unavailable")
    send(client, "/dna 7", "Granted 7 DNA")
    profileEffect(client, profile, "/dna", function(state) return state.dna == before.dna + 7 end)
    send(client, "/dna 0", "Syntax:")
    before = must(profile:Snapshot())
    send(client, "/dna 0x7", "Granted 7 DNA")
    profileEffect(client, profile, "/dna hexadecimal", function(state) return state.dna == before.dna + 7 end)
    before = must(profile:Snapshot())
    send(client, "/summon 1 0 0 0", "Summoned rigblock 1 to inventory")
    profileEffect(client, profile, "/summon", function(state) return state.part_count == before.part_count + 1 end)
    send(client, "/summon 0 0 0 0", "Syntax:")
end

local function resources(client)
    local initial = world(client)
    must(client.World:RestoreHero()) -- Fixture setup, not a /heal substitute.
    local maximum = world(client).hero.maximum_hit_point
    send(client, "/damage 17", "Damaging current hero by 17")
    waitWorld(client, initial, "/damage", function(state) return math.abs(state.hero.hit_point - (maximum - 17)) < 0.1 end)
    send(client, "/heal", "Healing all living squad members to full health")
    waitWorld(client, initial, "/heal", function(state) return state.hero.hit_point == state.hero.maximum_hit_point end)
    for _, verb in ipairs({"/power", "/mana"}) do
        must(client.World:RestoreHero())
        local power = world(client).hero.maximum_mana_point
        send(client, verb .. " -13", "Reducing current hero power by 13")
        waitWorld(client, initial, verb .. " reduce", function(state) return math.abs(state.hero.mana_point - (power - 13)) < 0.1 end)
        send(client, verb, "Restoring current hero to full power")
        waitWorld(client, initial, verb .. " fill", function(state) return state.hero.mana_point == state.hero.maximum_mana_point end)
        send(client, verb .. " 1", "Syntax:")
    end
    send(client, "/damage 0", "Syntax:")
    local stat = send(client, "/stat", "client-received")
    requireThat(client, string.match(stat.body, "^/stat %d+ %d+ %d+ %d+$") ~= nil,
        "commands-stat-normalize", "Fang did not attach native resource observations to /stat")
    local objectID, healthBits, powerBits, mask = string.match(stat.body, "^/stat (%d+) (%d+) (%d+) (%d+)$")
    requireThat(client, tonumber(objectID) == world(client).hero.object_id and tonumber(mask) == 3
        and tonumber(healthBits) > 0 and tonumber(powerBits) > 0,
        "commands-stat-cache", "Fang did not retain both received resource values for the controlled hero")
    requireThat(client, not string.find(stat.reply, "stale object=", 1, true),
        "commands-stat-stale", "/stat supplied a stale controlled object")
end

local function campaign(client)
    local initial = world(client)
    local original = initial.hero.position
    send(client, "/loc", "Position: X=")
    local destination = nil
    for _, offset in ipairs({{4, 0}, {0, 4}, {-4, 0}, {0, -4}}) do
        local path, pathError = client.World:Path(original,
            {x = original.x + offset[1], y = original.y + offset[2], z = original.z})
        if pathError == nil and path ~= nil and path.is_available and path.is_complete and not path.is_truncated then
            local point = path.goal.position
            if (point.x-original.x)^2 + (point.y-original.y)^2 > 4 then destination = point; break end
        end
    end
    requireThat(client, destination ~= nil, "commands-goto-fixture", "no distinct navigable teleport destination")
    send(client, string.format("/goto %.5f %.5f %.5f", destination.x, destination.y, destination.z), "Moving current hero to")
    waitWorld(client, initial, "/goto server", function(state)
        local p = state.hero.position
        return (p.x-destination.x)^2 + (p.y-destination.y)^2 + (p.z-destination.z)^2 < 0.04
    end)
    local nativeDeadline = scenario.Time() + 15000
    local isNativeArrived = false
    repeat
        local native = must(client.World:SnapshotNative()).gameplay_state
        isNativeArrived = native ~= nil and native.is_available and native.position ~= nil
            and (native.position.x-destination.x)^2 + (native.position.y-destination.y)^2 < 1
        if isNativeArrived then break end
        check(scenario.Sleep(100))
    until scenario.Time() >= nativeDeadline
    requireThat(client, isNativeArrived, "commands-goto-native", "native hero did not move to /goto destination")
    resources(client)
    local controlled = must(client.World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local actor = must(controlled:Snapshot())
    requireThat(client, #actor.supported_ability_names == 1, "commands-god-fixture", "gunner's attack is not uniquely supported")
    local health = world(client).hero.hit_point
    must(controlled:Cast(actor.supported_ability_names[1], initial.hero.object_id))
    waitWorld(client, initial, "god-off damage control", function(state) return state.hero.hit_point < health end)
    send(client, "/god", "God mode enabled")
    check(scenario.Sleep(5000))
    health = world(client).hero.hit_point
    must(controlled:Cast(actor.supported_ability_names[1], initial.hero.object_id))
    requireThat(client, math.abs(world(client).hero.hit_point-health) < 0.1,
        "commands-god-effect", "known gunner attack damaged an invulnerable hero")
    send(client, "/god", "God mode disabled")
    check(scenario.Sleep(5000))
    must(controlled:Cast(actor.supported_ability_names[1], initial.hero.object_id))
    waitWorld(client, initial, "/god off restores damage", function(state) return state.hero.hit_point < health end)
    must(controlled:Retire())
    must(client.World:RestoreHero())
    send(client, "/god", "God mode enabled") -- Keep the autonomous spawn from ending later diagnostics.
    local beforeIDs = {}
    for _, npc in ipairs(initial.npcs) do beforeIDs[npc.object_id] = true end
    local spawned = send(client, "/spawn CitadelBasicGunner", "Spawn queued:")
    requireThat(client, spawned.body == "/spawn CitadelBasicGunner.Noun", "commands-spawn-normalize",
        "Fang did not normalize the supported partial NPC name")
    waitWorld(client, initial, "/spawn", function(state)
        for _, npc in ipairs(state.npcs) do
            if not beforeIDs[npc.object_id] and npc.noun == "CitadelBasicGunner.Noun"
                and npc.is_published and not npc.is_dead then return true end
        end
        return false
    end)
    send(client, "/hint", "Hint:")
    send(client, "/scale 1.25 100 CitadelBasicGunner", "Preview 1.25x:")
    send(client, "/scale reset 100 CitadelBasicGunner", "Reset preview:")
    send(client, "/scale 0", "Syntax:")
    for _, asset in ipairs({"character_beam_in_plasma_electric", "character_beam_out_plasma_electric",
        "character_beam_in_bio", "character_beam_out_bio", "character_teleport_beam_out"}) do
        local receipt, cursor = send(client, "/effect " .. asset .. " world", "Previewing Fang world effect ")
        local observed = must(client.Chat:WaitNative(cursor, "effect_preview_override", 15000))
        requireThat(client, observed.value == 1 and observed.source == "native_client_trace",
            "commands-effect-native", "Fang did not accept the exact effect preview carrier")
    end
    send(client, "/effect character_beam_in_bio attached", "unsupported")
    send(client, "/effect no-such-effect", "unknown")
    for _, category in ipairs({"weapon", "hand", "foot", "offense", "defense", "utility"}) do
        local beforeDrop = world(client)
        requireThat(client, beforeDrop.is_pickup_available, "commands-drop-observer", "pickup observation unavailable")
        send(client, "/drop create " .. category, "Campaign equipment drop debug queued")
        waitWorld(client, initial, "/drop create " .. category, function(state)
            return state.is_pickup_available and state.equipment_drop_count == beforeDrop.equipment_drop_count + 1
        end)
    end
    local beforeDrop = world(client)
    send(client, "/drop create", "Campaign equipment drop debug queued")
    waitWorld(client, initial, "/drop create", function(state)
        return state.is_pickup_available and state.equipment_drop_count == beforeDrop.equipment_drop_count + 1
    end)
    send(client, "/drop create invalid", "Drop commands:")
    must(client.Diagnostics:Capture("commands-nonterminal-complete"))
    check(scenario.Log("ACCEPTANCE /scale: server preview results and captures retained; native pixel size is not asserted"))
end

return function()
    check(scenario.RequireAPI(1))
    local server = must(scenario.StartServer({}))
    local profile = must(server:CreateProfile({name = "commands", is_admin = true,
        hero_nouns = {1667741389, 3618328019, 749013658}, chain_progression = 1}))
    local client = must(profile:Launch({client_id = "commands", launch_mode = "normal",
        screen_width = 1280, screen_height = 720, native_ui_scale_percent = 100,
        is_intro_skipped = true, is_cinematic_skipped = true}))
    check(client:SetTimeout(90000))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    ship(client, profile)
    must(client.World:Configure({run_seed = 191192, is_ambient_population_suppressed = true}))
    must(client.Scene:Set(scene.Navigation))
    must(client.Mission:Enter(1))
    local mission = must(profile:Snapshot())
    requireThat(client, mission.is_noreplay and not mission.is_replay_randomized,
        "commands-noreplay-mission", "login toggle did not apply to the actual mission")
    diagnostics(client)
    campaign(client)
    -- The inventory found this server-owned informational verb missing from
    -- Fang's bridge. Keep it last so its missing reply cannot hide other cases.
    -- This is a regression assertion, not permission to add a client hook.
    send(client, "/taunt", "unavailable")
    check(scenario.Log("COMPLETE commands.lua; terminal and multiplayer coverage are separate scripts; no aggregate all-commands verdict"))
    return nil
end
