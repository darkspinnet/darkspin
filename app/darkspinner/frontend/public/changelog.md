# Changelog

### 2026-09-20

- Reannounce teammate player slots when rejoining co-op so their hero health bars have valid owning-player records in the new client.
- Drive the following player's own hero through reliable locomotion updates because ordinary movement replication is ignored for locally controlled heroes.
- Reload mission resources before sending a retained co-op rejoin snapshot on Continue, and restore defeated heroes with their death pose instead of a living beam-in.
- Deliver the final hero's death presentation to teammates after a party wipe instead of leaving that hero visibly alive, and flush queued removals before mission failure.
- Wait until every connected party member's entire squad is defeated before showing mission failure, keeping surviving allies and enemies active after an individual squad wipe.
- Remove Sage's Dendrones from every teammate's view when he dies, including deaths caused by NPCs simulated through another party member.
- Remove collected DNA pickups from teammates' views and show the collector's pickup effect when an ally gathers them.
- Send Ride the Lightning's authored start animation to teammates along with its teleport so allies can see the cast.
- Refresh ally-follow movement between input commands, maintain a three-unit gap, and send the navigation-resolved destination to the following client and teammates.
- Forward /follow through Fang to the server's ally-follow command instead of rejecting it as unrecognized.
- Replicate attack pursuit movement to teammates immediately when a hero starts approaching an out-of-range enemy.
- Synchronize deployed heroes' rendered positions with the server's placement after local and teammate creation, including mission entry and reconnect rosters.
- Show Continue and Start Fresh for live campaign sessions as well as saved checkpoints, reattach live membership after login, and preserve other party members when starting fresh.
- Send every campaign party member's identity and squad before completing the multiplayer roster merge, preserving each client's loading status.

### 2026-09-19

- Restore missing Nightmare Vines as destructible fixtures at their authored positions and scales, preserving the roots beneath them from the same Nocturna map variant.
- Publish hero combat-state transitions so the client's authored victory-idle animations can play after fights, while preserving stealth state.
- Keep Healing Sprite healing ticks running when it follows its hero by accepting follow updates without scheduled-arrival metadata.
- Clear Pterodyne's movement goal after its scream before returning animation control to idle during cooldown.

### 2026-09-18

- Preserve removed campaign squad members across relaunches by leaving saved empty slots empty during login repair.
- Stop automatically duplicating campaign heroes into PvP squads, repair overlapping PvP assignments on login, and save explicitly emptied squads so removed heroes remain available in the Arsenal.
- Include Arsenal slot model IDs, suppression state and pending model resources in snapshots to diagnose invisible heroes with an intact catalogue.
- Avoid server startup timeouts by loading ability coefficients once instead of repeatedly scanning the content cache for each token.
- Correct the Arsenal snapshot controller address and capture catalogue counts even when the collection UI cannot be read.
- Capture Arsenal collection counts, filters and scroll position in manual snapshots even while the gameplay clock is inactive.
- Use available saved appearance revisions in Arsenal account, deck and hero responses so legacy saves do not request nonexistent image revisions; use noun templates when the saved appearance is missing.
- Synchronize enemy pull and knockback endpoints with the client physics mover, cancel stale melee pursuit, and refresh current health and power after the forced reaction.
- Play a death animation for killed Dendrones before removing their corpses, while preserving their existing respawn delay.
- Keep editor image filenames, saved hero revisions and save responses synchronized, reserve revisions across accounts to prevent appearance overwrites, and log rejected saves.
- Restore Wraith's Pummel impact event when recovering its melee definition so accepted hits include the authored visual and sound effects.
- Create a safe mission checkpoint after deployment so Continue is available before the first defeated enemy group or pickup, including newly initialized co-op members.
- Seed players joining an already-populated co-op zone with existing NPC positions, facing, resources and targets, instead of sending only later updates for enemies they never received.
- Honor party leave requests followed by trailing shutdown RPCs, remove disconnected heroes and companions from teammates' scenes immediately, and restore them on successful reconnect.
- Hold co-op NPC spawns and world updates until each player's dungeon scene is ready, and preserve queued packets across loading transitions so enemies cannot attack a client that missed their spawn.
- Send co-op teammates the same beam-in position, effect and animation after hero creation, and keep missing player entry markers near the current map's entrance.
- Preserve teammate readiness during hero roster refreshes instead of resetting already-entered players to loading and causing misleading cinematic wait messages.
- Deliver delayed NPC movement, attacks and effects to every campaign teammate, preserve spawn-before-movement ordering, and use the actual shared pursuit target instead of reconstructing it per player.
- Add an Open Bug Folder link at the top left of the report dialog, available before creating a report.
- Resolve multiplayer hero appearances from available saved image versions instead of stat revisions, falling back to the hero's shipped template when the saved appearance is missing.
- Remove a quitting player from the multiplayer party when their leave request is followed by disconnect, and notify teammates of the removal and any leader change.
- Synchronize enemy target and combat state with teammates, and deliver pursuit arrivals and Arc Welding Melee attack continuations to every player in the zone.
- Show catalyst pickup poses and animations to teammates, and synchronize player-indexed catalyst inventories and link bonuses after pickups, moves and drops.

### 2026-09-17

- Infer Darkrun conversion destinations from package names: Creatures.package extracts to Creatures.ds, and Creatures.ds repacks to Creatures.package.
- Link to the vgmstream GitHub releases page when Darkrun audio conversion cannot find vgmstream-cli.
- Preserve each companion attack's damage classification and position, carry Sprout's poison element into resistance checks, and stop applying area mitigation to Expunge's single-target remaining-damage burst.
- Fix multiplayer Thorn Bark reflection and attribute its damage, rewards and feedback to the struck hero; prevent life-drain healing after a damage reaction kills the enemy.
- Honor the struck hero's debuff immunity for multiplayer enemy poison, vulnerabilities and control effects, and apply only the strongest overlapping Crushing Dread aura to campaign damage.
- Apply catalyst primary-stat bonuses to damage and healing, propagate elemental damage and defense-based attack bonuses through inventory changes, and respect shield direction for Thorn Bark reflection.
- Preserve pet and burn damage classifications and combined area/periodic defenses, apply companions' own defense ratings, honor finite TC shields in duels, and keep scripted deaths independent of combat resistance.
- Apply enemy physical and energy defense ratings with difficulty scaling, cap ordinary stacked mitigation at 50% and temporary damage reduction at 75%, and preserve scripted immunity phases.
- Apply Soul Link damage before selecting a replacement hero, prevent hit reactions on replacements, enable defensive hit stacks in duels, and include ally auras in companions' mitigation cap.
- Fix shifted equipment suffix stats that incorrectly granted extreme damage reduction and misapplied physical and energy modifiers; refresh the server content cache automatically.
- Fix Shadow Doppler effects, The Corruptor's combat behavior, and Enemy Portal visuals; rebalance stacked defenses to prevent immunity.
- Improve windowed and borderless modes, resolution changes, saved settings, and HUD resizing; restore Enter-to-chat.
- Fix tutorial loading, XP display, Return to Ship, and squad unlocks; default new Crogenitors to skipping the tutorial while keeping it available.
- Improve launcher bug reporting, icons, version display, and update progress; disable cinematic skipping.
- Expand releases to more Windows, macOS, and Linux architectures, add standalone Darkrun downloads, and simplify unstable build labels.
- Strengthen security with dependency updates, safer content parsing, authentication hardening, and private vulnerability reporting.

### 2026-09-16

- Prepare the darkspinnet public launch with permanent download links, stable release ZIPs, and unstable builds with Windows self-updates.
- Fix duel lobbies stalling before squad selection.

### 2026-09-07

- Change Invincitron's hover drone from remaining at its owner's position to circling above him, with its firing position following the same orbit.
- Restore Nashira's Shadow Panic shriek on her melee/lob attack paths, with timed nearby fear, a fifteen-second cooldown, and normal attacks resuming after the cast.
- Change Nashira illusion deaths from the full boss death animation to a brief duplication-effect disappearance; preserve the real boss's death sequence.

- Fix boss-summoned Exploder Scarabs failing damage and explosion processing because they were incorrectly treated as boss-wave actors.
- Change Laser Tank beams from following heroes to fixed placement anchors, with explicit effect cleanup when firing ends or is interrupted.
