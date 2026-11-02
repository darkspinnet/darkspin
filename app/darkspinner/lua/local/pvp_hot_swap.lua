---One owned duel reused for installed hero replacements and resource resets.
---Server fixtures are distinguished from native input and rendering evidence.
local must,check = scenario.Must,scenario.Check
local function pause(ms) check(scenario.Sleep(ms)) end

local function capture(clients,label)
    for _,client in ipairs(clients) do
        must(client.Diagnostics:Capture(label))
        must(client.UI:Screenshot(label))
    end
end

local function launch(server,name,mode)
    local profile=must(server:CreateProfile({name=name,hero_nouns={171910024,185979955,700180862},chain_progression=1,is_pvp_enabled=true}))
    local client=must(profile:Launch({client_id=name,launch_mode=mode}))
    must(client:Login())
    must(client.Scene:Wait(scene.Arsenal))
    return client,profile
end

local function ledger(world)
    local total=0
    for _,member in ipairs(world.arena.members) do
        total=total+member.damage_done+member.damage_taken+member.healing_done+member.healing_received
    end
    return total
end

local function run(clients)
    local server=must(scenario.StartServer({}))
    local first,firstProfile=launch(server,"swap-first","normal")
    clients[1]=first
    local second,secondProfile=launch(server,"swap-second","detached")
    clients[2]=second
    must(first.Party:Create("swap-duel"))
    must(first.Party:Invite(second))
    must(second.Party:Join(first))
    must(first.Scene:Set(scene.Navigation))
    must(second.Scene:Set(scene.Navigation))
    must(firstProfile:WaitForReportedLocation("navigation",15000))
    must(secondProfile:WaitForReportedLocation("navigation",15000))
    local match=must(server:StartPVP({teams={{first},{second}}}))
    check(scenario.Parallel({
        first=function() must(first.PVP:Accept()); return nil end,
        second=function() must(second.PVP:Accept()); return nil end,
    }))
    must(first.PVP:Wait())
    must(second.PVP:Wait())
    local initial=must(first.World:SnapshotLiving())
    local expectedLedger=ledger(initial)
    -- Also replace a nonzero deployed slot: the controlled object must remain
    -- the selected slot instead of being reset to the roster's first hero.
    must(first.World:SelectHero(1))
    local selected=must(first.World:SnapshotLiving())
    local startNative=must(first.World:SnapshotNative()).gameplay_state
    local move=must(first.World:Click({x=startNative.position.x+2,y=startNative.position.y,z=startNative.position.z}))
    if not move.is_processed then error("native placement input unprocessed; no replay") end
    pause(700)
    local nouns={700180862,171910024,185979955,700180862}
    for _,noun in ipairs(nouns) do
        local before=must(first.World:SnapshotLiving())
        local beforeNative=must(first.World:SnapshotNative()).gameplay_state
        local receipt=must(first.World:ReplaceHero(noun))
        if not receipt.is_queued or not receipt.is_replacement or receipt.recipient_count~=2 then
            error("replacement projection was not queued to both owned clients")
        end
        pause(1200)
        for _,client in ipairs(clients) do
            local world=must(client.World:SnapshotLiving())
            local native=must(client.World:SnapshotNative()).gameplay_state
            if world.game_id~=match.game_id or native==nil or not native.is_world_active
                or native.controlled_handle==nil or native.controlled_handle==0 then
                error("replacement lost an active native participant")
            end
        end
        local after=must(first.World:SnapshotLiving())
        local afterNative=must(first.World:SnapshotNative()).gameplay_state
        if after.hero.noun~=noun or after.hero.index~=selected.hero.index
            or after.hero.object_id~=selected.hero.object_id or ledger(after)~=expectedLedger then
            error("replacement changed the slot, object identity or combat ledger")
        end
        if math.abs(afterNative.position.x-beforeNative.position.x)>0.2
            or math.abs(afterNative.position.y-beforeNative.position.y)>0.2 then
            error("replacement unexpectedly moved the hero")
        end
        must(first.World:RestoreHero())
        must(second.World:RestoreHero())
        pause(250)
        for _,client in ipairs(clients) do
            local world=must(client.World:SnapshotLiving())
            if world.hero.hit_point~=world.hero.maximum_hit_point
                or world.hero.mana_point~=world.hero.maximum_mana_point or ledger(world)~=expectedLedger then
                error("resource restore failed or changed combat statistics")
            end
        end
        check(scenario.Log("Reused active arena game="..match.game_id.." slot="..after.hero.index.." noun="..noun))
        capture(clients,"pvp-hot-swap-"..noun)
        if noun==185979955 then
            -- Use the real native Reconstruct support on the replaced Meditron.
            -- Its temporary capacity creates a healable deficit without a
            -- synthetic damage event or another match setup.
            local beforeCast=must(first.World:SnapshotLiving())
            must(first.UI:Click(670,655))
            pause(150)
            local native=must(first.World:SnapshotNative()).gameplay_state
            local cast=must(first.World:Click(native.position))
            if not cast.is_processed then error("support input unprocessed; no replay") end
            pause(4300)
            local afterCast=must(first.World:SnapshotLiving())
            if afterCast.hero.mana_point>=beforeCast.hero.mana_point
                or afterCast.hero.maximum_hit_point<=beforeCast.hero.maximum_hit_point
                or afterCast.hero.hit_point<=beforeCast.hero.hit_point then
                error("replacement Meditron did not execute native Reconstruct capacity and healing ticks")
            end
            local healed=afterCast.hero.hit_point-beforeCast.hero.hit_point
            if math.abs(ledger(afterCast)-expectedLedger-2*healed)>0.05 then
                error("Reconstruct health gain disagrees with actual Arena healing statistics")
            end
            expectedLedger=ledger(afterCast)
            must(first.World:RestoreHero())
            pause(250)
            local restored=must(first.World:SnapshotLiving())
            if restored.hero.hit_point~=restored.hero.maximum_hit_point
                or restored.hero.mana_point~=restored.hero.maximum_mana_point or ledger(restored)~=expectedLedger then
                error("post-ability restore failed or added synthetic healing credit")
            end
            check(scenario.Log("Native Reconstruct healed="..healed.." then RestoreHero replenished Power without changing its ledger"))
            capture(clients,"pvp-hot-swap-reconstruct")
        end
    end
    -- A native swap away and back must still work after noun replacement.
    must(first.World:SelectHero(0))
    pause(5200)
    must(first.World:SelectHero(1))
    capture(clients,"pvp-hot-swap-redeployment")
    check(scenario.Log("Four hero replacements, native Reconstruct, nine restores and normal redeployment completed in one duel"))
end

return function()
    check(scenario.RequireAPI(1))
    local clients={}
    local isComplete,failure=pcall(run,clients)
    if not isComplete then
        for _,client in ipairs(clients) do
            local result,captureError=client.Diagnostics:Capture("pvp-hot-swap-failed")
            check(scenario.Log("swap failure manifest="..tostring(result and result.manifest_path).." error="..tostring(captureError)))
        end
        error(tostring(failure))
    end
    return nil
end
