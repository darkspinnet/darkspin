local must,check=scenario.Must,scenario.Check
local function pause(ms) check(scenario.Sleep(ms)) end
local function launch(server,id,noun)
 local profile=must(server:CreateProfile({name=id,hero_nouns={noun,171910024,700180862},chain_progression=2,is_pvp_enabled=true}))
 local client=must(profile:Launch({client_id=id,launch_mode='detached'}))
 must(client:Login()); must(client.Scene:Wait(scene.Arsenal)); return client
end
local function cast(client,x,label)
 local before=must(client.World:SnapshotLiving())
 must(client.World:RestoreHero({hit_point=before.hero.maximum_hit_point*0.4}))
 must(client.World:ResetCooldowns())
 local point=must(client.World:SnapshotNative()).gameplay_state.position
 must(client.UI:Click(x,655)); pause(150); must(client.World:Click(point))
 pause(4500)
 local after=must(client.World:SnapshotLiving())
 check(scenario.Log(label..' alive noun='..after.hero.noun..' hp='..after.hero.hit_point..' power='..after.hero.mana_point))
 must(client.Diagnostics:Capture(label))
end
return function()
 local server=must(scenario.StartServer({}))
 local first=launch(server,'rig-medic',185979955)
 local second=launch(server,'rig-vex',3618328019)
 must(first.Party:Create('rig-fresh')); must(first.Party:Invite(second)); must(second.Party:Join(first))
 must(first.Scene:Set(scene.Navigation));must(second.Scene:Set(scene.Navigation))
 must(first:WaitForReportedLocation('navigation',15000));must(second:WaitForReportedLocation('navigation',15000))
 must(server:StartPVP({teams={{first},{second}}}))
 check(scenario.Parallel({first=function() must(first.PVP:Accept()) end,second=function() must(second.PVP:Accept()) end}))
 must(first.PVP:Wait()); must(second.PVP:Wait())
 check(scenario.Parallel({medic=function() cast(first,640,'fresh-medic-self') end,vex=function() cast(second,605,'fresh-vex-self') end}))
 pause(10000)
 must(first.World:SnapshotLiving());must(second.World:SnapshotLiving())
 check(scenario.Log('Fresh installed heroes survived both authored bone effects without replacement'))
end
