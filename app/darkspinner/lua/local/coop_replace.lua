---Fresh owned campaign replacement verification; ordinary native input, no protected yielding calls.
local must,check=scenario.Must,scenario.Check
local participantCount=2
local suite=participantCount==2 and "coop_replace" or "solo_replace"
local nouns={1667741389,3524487169,1557965449,749013658,1667741389}
local function pause(ms) check(scenario.Sleep(ms)) end
local function distance(first,second)
    local x,y=first.x-second.x,first.y-second.y
    return math.sqrt(x*x+y*y)
end
local function offset(point,x,y) return {x=point.x+x,y=point.y+y,z=point.z} end
local function jsonString(text)
    return '"'..tostring(text):gsub('[%z\1-\31\\"]',function(character)
        return string.format("\\u%04x",string.byte(character))
    end)..'"'
end
local function json(item)
    if item==nil then return "null" end
    if type(item)=="string" then return jsonString(item) end
    if type(item)=="number" or type(item)=="boolean" then return tostring(item) end
    if #item>0 then
        local entries={}
        for _,element in ipairs(item) do entries[#entries+1]=json(element) end
        return "["..table.concat(entries,",").."]"
    end
    local keys,entries={},{}
    for key in pairs(item) do keys[#keys+1]=key end
    table.sort(keys)
    for _,key in ipairs(keys) do entries[#entries+1]=jsonString(key)..":"..json(item[key]) end
    return "{"..table.concat(entries,",").."}"
end
local function record(event)
    event.suite,event.at_ms=suite,scenario.Time()
    local line=json(event)
    if #line>2048 then error("status log exceeds durable boundary") end
    check(scenario.Log(line))
end
local function read(client,baseline)
    local world=must(client.World:SnapshotLiving())
    local probe=must(client.World:SnapshotNative())
    local native=probe.gameplay_state
    if not world.campaign or #world.campaign.members~=participantCount or world.zone_generation==0
        or world.peer_generation==0 or world.transport_generation==0 or not native or not native.is_world_active
        or not native.is_rig_ready or not native.position or probe.pid~=client.PID or probe.instance_id~=client.InstanceID then
        error("committed owned campaign/native rig unavailable")
    end
    if baseline and (world.game_id~=baseline.game_id or world.zone_generation~=baseline.zone_generation
        or world.peer_generation~=baseline.peer_generation or world.transport_generation~=baseline.transport_generation
        or world.user_id~=baseline.user_id) then error("campaign world epoch changed; no replay") end
    return world,native
end
local function actor(client,baseline,phase)
    local world,native=read(client,baseline)
    record({event="actor",phase=phase,client_id=client.ID,pid=client.PID,instance_id=client.InstanceID,
        user_id=world.user_id,game_id=world.game_id,zone_generation=world.zone_generation,
        peer_generation=world.peer_generation,transport_generation=world.transport_generation,
        object_id=world.hero.object_id,index=world.hero.index,noun=world.hero.noun,
        hp=world.hero.hit_point,mp=world.hero.mana_point,server_position=world.hero.position,
        native_position=native.position,native_frame=native.frame_sequence,is_rig_ready=native.is_rig_ready})
    return world,native
end
local function clickUI(client,x,y)
    local receipt=must(client.UI:Click(x,y))
    if not receipt.is_processed then error("native menu input unprocessed; no replay") end
end
local function menu(client,x,y) clickUI(client,x,y);pause(600) end
local function inviteParty(clients)
    local leader,follower=clients[1],clients[2]
    menu(leader,105,35);menu(leader,368,182);menu(leader,930,265)
    clickUI(follower,604,53)
    local deadline=scenario.Time()+12000
    while scenario.Time()<deadline do
        local first=must(leader.Party:Snapshot()).party
        local second=must(follower.Party:Snapshot()).party
        if first.id~=0 and first.id==second.id and #first.members==2 and #second.members==2 then
            menu(leader,759,498);menu(leader,1036,103);return
        end
        pause(150)
    end
    error("native Social invitation did not establish owned two-member party")
end
local function launch(server,index)
    local id=suite..(index==1 and "-source" or "-observer")
    local lead=index==1 and 1667741389 or 700180862
    local profile=must(server:CreateProfile({name=id,hero_nouns={lead,117394854,576185321},chain_progression=1}))
    local client=must(profile:Launch({client_id=id,launch_mode=index==1 and "normal" or "detached"}))
    must(client:Login());must(client.Scene:Wait(scene.Arsenal))
    local window=must(client.UI:InspectWindow()).window
    if not window or window.width~=1280 or window.height~=720 then error("native Social/hotbar requires1280x720") end
    return client
end
local function enter(clients)
    if participantCount==2 then inviteParty(clients) end
    for _,client in ipairs(clients) do
        must(client.World:Configure({run_seed=104729,is_ambient_population_suppressed=true}))
        must(client.Scene:Set(scene.Navigation))
    end
    if participantCount==1 then must(clients[1].Mission:Enter(1));return end
    must(clients[1].Mission:Select(1));must(clients[1].Mission:Start())
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Continue());return nil end,
        second=function() must(clients[2].Mission:Continue());return nil end,
    }))
    check(scenario.Parallel({
        first=function() must(clients[1].Mission:Wait());return nil end,
        second=function() must(clients[2].Mission:Wait());return nil end,
    }))
end
local function fixtures(client)
    local resource=must(client.World:RestoreHero())
    local cooldown=must(client.World:ResetCooldowns())
    if not resource.is_queued or resource.recipient_count~=participantCount or not cooldown.is_cooldown_reset
        or not cooldown.is_queued or cooldown.recipient_count~=participantCount then error("campaign resource/CD fixture publication incomplete") end
end
local function verifyRosters(clients,baselines,expectedNouns,phase)
    for index,client in ipairs(clients) do
        local world=actor(client,baselines[index],phase)
        for participant,baseline in ipairs(baselines) do
            local found
            for _,member in ipairs(world.campaign.members) do
                if member.user_id==baseline.user_id then
                    if found or not member.is_ready or member.peer_generation~=baseline.peer_generation
                        or member.transport_generation~=baseline.transport_generation or member.zone_generation~=baseline.zone_generation
                        or member.deployed_index~=0 then error("campaign participant metadata changed or is incomplete") end
                    local character
                    for _,hero in ipairs(member.characters) do if hero.index==0 then character=hero end end
                    if not character or character.noun~=expectedNouns[participant] or character.is_defeated or character.hit_point<=0 then
                        error("replacement noun/resource not projected to every campaign observer")
                    end
                    found=true
                end
            end
            if not found then error("campaign observer omitted owned participant") end
        end
    end
end
local function serverPosition(world)
    if world.hero.is_movement_available and world.hero.movement_position then return world.hero.movement_position end
    return world.hero.position
end
local function movement(client,baseline,phase)
    local before,nativeBefore=read(client,baseline)
    local goal
    for _,delta in ipairs({{0,4},{4,0},{0,-4},{-4,0}}) do
        local requested=offset(nativeBefore.position,delta[1],delta[2])
        local path=must(client.World:Path(nativeBefore.position,requested))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal and path.goal.position then
            local point=path.goal.position
            local isClear=true
            for _,npc in ipairs(before.npcs) do
                if not npc.is_dead and distance(npc.position,point)<2.5 then isClear=false end
            end
            if isClear and distance(point,nativeBefore.position)>2 then
                local projection=must(client.World:Project(point)).projection
                if projection and projection.is_available and projection.is_on_screen then goal=point;break end
            end
        end
    end
    if not goal then error("no bounded complete native movement probe path") end
    local receipt=must(client.World:Hold(goal,650))
    local poll=receipt.gameplay_click
    if not receipt.is_processed or not poll or not poll.is_press_poll_observed or not poll.is_release_poll_observed then
        error("native movement input outcome unknown; no replay")
    end
    local after,nativeAfter=read(client,baseline)
    local nativeDistance=distance(nativeBefore.position,nativeAfter.position)
    local serverDistance=distance(serverPosition(before),serverPosition(after))
    record({event="movement",phase=phase,client_id=client.ID,pid=client.PID,noun=after.hero.noun,goal=goal,
        native_before=nativeBefore.position,native_after=nativeAfter.position,
        server_before=serverPosition(before),server_after=serverPosition(after),
        native_distance=nativeDistance,server_distance=serverDistance,
        frame_before=nativeBefore.frame_sequence,frame_after=nativeAfter.frame_sequence,
        is_native_ground_query_success=poll.is_native_ground_query_success,chosen_ground_point=poll.chosen_ground_point})
    if nativeAfter.frame_sequence<=nativeBefore.frame_sequence or nativeDistance<.5 or serverDistance<.5 then
        error("replacement hero did not move through native/server path")
    end
end
local function npcStable(npc,initial,phase)
    local current=must(npc:Snapshot())
    local first,second=initial.identity,current.identity
    if not current.is_available or current.is_ai_enabled or current.is_dead or current.hit_point~=initial.hit_point
        or not second or first.object_id~=second.object_id or first.spawn_request_id~=second.spawn_request_id
        or first.user_id~=second.user_id or first.game_id~=second.game_id or first.zone_generation~=second.zone_generation
        or first.peer_generation~=second.peer_generation or first.transport_generation~=second.transport_generation then
        error("replacement invalidated or altered the pinned controlled NPC")
    end
    record({event="controlled_npc_preserved",phase=phase,object_id=npc.ObjectID,identity=second,hp=current.hit_point})
end
local function replace(client,noun,baseline,phase)
    local before,nativeBefore=actor(client,baseline,phase.."-before")
    record({event="replacement_start",phase=phase,client_id=client.ID,pid=client.PID,
        old_noun=before.hero.noun,new_noun=noun,object_id=before.hero.object_id})
    local receipt=must(client.World:ReplaceHero(noun))
    if not receipt.is_replacement or not receipt.is_rig_ready or not receipt.is_queued
        or receipt.recipient_count~=participantCount or receipt.zone_generation~=baseline.zone_generation then
        error("campaign replacement receipt incomplete; no replay")
    end
    local after,nativeAfter=actor(client,baseline,phase.."-after")
    if after.hero.index~=0 or after.hero.noun~=noun or nativeAfter.frame_sequence<=nativeBefore.frame_sequence then
        error("replacement actor/native rig did not advance")
    end
    record({event="replacement_end",phase=phase,client_id=client.ID,object_id=after.hero.object_id,noun=noun,
        zone_generation=receipt.zone_generation,recipient_count=receipt.recipient_count,is_rig_ready=receipt.is_rig_ready})
end
local function targetByID(world,objectID)
    for _,npc in ipairs(world.npcs) do if npc.object_id==objectID then return npc end end
    return nil
end
local function finishCast(clients,baselines,npc)
    local source=clients[1]
    fixtures(source)
    local sourceWorld,native=read(source,baselines[1])
    local goal
    for _,delta in ipairs({{6,0},{0,6},{-6,0},{0,-6}}) do
        local path=must(source.World:Path(native.position,offset(native.position,delta[1],delta[2])))
        if path.is_available and path.is_complete and not path.is_truncated and path.goal then goal=path.goal.position;break end
    end
    if not goal then error("no complete controlled NPC staging path") end
    local moved=must(npc:Move(goal))
    if not moved.is_accepted or not moved.is_completed or not moved.projected_destination then error("controlled NPC normal movement incomplete") end
    local beforeNPC=must(npc:Snapshot())
    if not beforeNPC.is_available or beforeNPC.is_dead or beforeNPC.is_ai_enabled then error("controlled NPC unavailable before native cast") end
    local availability=must(source.Ability:Snapshot()).state
    if not availability.is_available or not availability.is_supported or availability.ability_asset~="LightningRogueActive" then
        error("restored Blitz lacks supported Ride the Lightning producer")
    end
    local result=must(source.Ability:Cast(beforeNPC.position))
    local cast=result.observation and result.observation.state.cast
    if not result.is_completed or not cast or cast.is_failed or not cast.is_accepted or not cast.is_arrived
        or not cast.is_arrival_processed or not cast.is_impact_committed or not cast.is_released or cast.damage<=0
        or result.observation.state.last_revision~=availability.last_revision+1 then error("native Ride the Lightning lacked exact positive terminal cast") end
    local afterNPC=must(npc:Snapshot())
    if not afterNPC.is_available or not afterNPC.is_dead or afterNPC.hit_point>0 then error("controlled target death unobserved") end
    for index,client in ipairs(clients) do
        local world=read(client,baselines[index])
        local observed=targetByID(world,npc.ObjectID)
        if not observed or not observed.is_dead or observed.hit_point>0 then error("shared target death absent from campaign observer") end
    end
    record({event="native_cast",ability_name="Ride the Lightning",asset=cast.ability_asset,
        target_object_id=npc.ObjectID,damage=cast.damage,revision=cast.revision,is_target_dead=true,
        source_object_id=sourceWorld.hero.object_id,is_native_packet_delivery_verified=false})
end
return function()
    check(scenario.RequireAPI(1))
    local server=must(scenario.StartServer({}))
    local clients={}
    if participantCount==2 then
        check(scenario.Parallel({
            source=function() clients[1]=launch(server,1);return nil end,
            observer=function() clients[2]=launch(server,2);return nil end,
        }))
    else clients[1]=launch(server,1) end
    enter(clients)
    local baselines,expectedNouns={},{}
    for index,client in ipairs(clients) do
        must(client.World:Ready());baselines[index]=read(client)
        expectedNouns[index]=baselines[index].hero.noun
        if baselines[index].game_id~=baselines[1].game_id or baselines[index].zone_generation~=baselines[1].zone_generation then
            error("participants entered different committed campaign worlds")
        end
        local capture=must(client.Diagnostics:Capture(suite.."-entry"))
        record({event="diagnostic",client_id=client.ID,pid=client.PID,manifest=capture.manifest_path})
    end
    local npc=must(clients[1].World:SpawnControlledNPC("CitadelBasicGunner.Noun"))
    local initialNPC=must(npc:Snapshot())
    if not initialNPC.is_available or initialNPC.is_ai_enabled or initialNPC.is_dead then error("controlled NPC constructor unavailable") end
    for index,noun in ipairs(nouns) do
        local phase="source-"..index
        replace(clients[1],noun,baselines[1],phase)
        expectedNouns[1]=noun
        verifyRosters(clients,baselines,expectedNouns,phase)
        npcStable(npc,initialNPC,phase)
        fixtures(clients[1]);movement(clients[1],baselines[1],phase)
        if index==3 and participantCount==2 then
            replace(clients[2],185979955,baselines[2],"observer")
            expectedNouns[2]=185979955
            verifyRosters(clients,baselines,expectedNouns,"observer")
            npcStable(npc,initialNPC,"observer");fixtures(clients[2]);movement(clients[2],baselines[2],"observer")
        end
    end
    for _,slot in ipairs({1,0}) do
        fixtures(clients[1]);must(clients[1].World:SelectHero(slot,20000))
        local world=actor(clients[1],baselines[1],"ordinary-select-"..slot)
        if world.hero.index~=slot then error("ordinary native hero selection failed after replacements") end
        npcStable(npc,initialNPC,"ordinary-select-"..slot)
    end
    verifyRosters(clients,baselines,expectedNouns,"before-native-cast")
    finishCast(clients,baselines,npc)
    local frames={}
    for index,client in ipairs(clients) do local world,native=read(client,baselines[index]);frames[index]=native.frame_sequence end
    pause(100)
    for index,client in ipairs(clients) do
        local world,native=actor(client,baselines[index],"final")
        if native.frame_sequence<=frames[index] then error("final native frame stalled") end
        local capture=must(client.Diagnostics:Capture(suite.."-final"))
        record({event="diagnostic",client_id=client.ID,pid=client.PID,manifest=capture.manifest_path})
        must(client.UI:Screenshot(suite.."-final-"..index))
    end
    record({event="suite_end",status="passed",source_replacements=5,observer_replacements=participantCount==2 and 1 or 0,
        live_client_count=participantCount,is_native_packet_delivery_verified=false,is_saved_deck_verified=false})
end

