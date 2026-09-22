# Changelog

### 2026-09-21

- Give converted audio streams deterministic `ds_` aliases derived from event, inherited parent, loop, and shared-reference context; organize effects into family folders such as `effect/sfx` and `effect/scom`; annotate each WAV's DSE with searchable source keys, pointer roles, tags, and every AudioProps reference; consolidate numbered WAV derivatives with their event property list in one reconstructable DSE; and accelerate conversion with parallel WAV decoding and direct package streaming while reporting elapsed time.
- Persist each Crogenitor's creation timestamp, show it on launcher profile cards, and record the latest successful remote connection date beside its server address.
- Record the last local launch for each Crogenitor and show profile level and last-played date in local, Detached, and Remote selectors.
- Cache remote Crogenitor and server metadata locally, refresh servers in the background every two days, and refresh profile progress when a launched remote game exits.
- Move Botanical Tunnelers toward their target during the visible underground burrow, then emerge with their poison attack and wait through the authored cooldown before burrowing again.
- Restore Goliath's Shockwave to its full authored 4-metre reach and 6-metre hit arc, and let Zetawatt Beam pierce every enemy along its 35-metre path regardless of aggro target.
- Use Nightmare Vines' authored dead graphics state without overlaying generic creature-death and Zelem explosion effects.
- Tag locally built Darkspinner versions with the current seven-character Git commit, such as `1.0.4-dev-db3367e`, while preserving stable and unstable release versioning.
- Change the launcher readiness message from `DarkSpinner ready` to `DarkSpinner is ready`.
- Limit Remote, Detached, LAN multiplayer, and server-port controls to development and unstable Darkspinner builds while keeping production focused on Launch and Config.
- Match the Detached Crogenitor selector spacing and card inset to the Launch and Remote selectors.
- Give Remote a blue tab, portrait, profile-label, and Play accent and Detached a matching purple accent while retaining green for Launch.
- Reformat launcher profile details, including the remote server address, into aligned property and regular-weight value rows with dividers between fields, concise numeric Crogenitor levels, and level badges on selected portraits.
- Add `/drop` command help and `/drop create [weapon|hand|foot|offense|defense|utility]` to generate a collectible campaign item from the current map, difficulty, nearby enemy context, active squad, and optional equipment category while reporting every drop roll in chat.
- Give every campaign run a unique persisted loot seed, derive mission streams from the map and difficulty, and restore the exact drop random state when continuing from a checkpoint.
- Replace the launcher's custom UI with shadcn-vue in dark mode with an emerald green accent, deep green action buttons, centered underline navigation, a wider launch panel, grouped profile controls, profile information cards, compact headerless configuration cards, keyboard-accessible menus, and themed dialogs.
- Replace the Field Manual placeholder with current campaign controls, mission flow, party setup, prominent bug reporting, recovery commands, and a separate developer-tools reference.
- Encode Campaign leaderboard progression with separate threat and star ranks so levels display correctly instead of values such as `0-4★3`.
- Round profile and Campaign leaderboard Kill/Death Ratio values to two decimal places.
- Give boss equipment drops a rare chance to use the Hyperspatial Protector, Galactic Eviscerator, or Darkmatter Starhelm base with mission-scaled levels, rarity, and affixes.
- Restore Quantum Blink's authored slide animation, five randomized strike poses, one-second ending pose, and final animation reset.
- Keep each co-op campaign member active after another player enters or aborts from victory results, allowing every ally to finish their own Beam Out instead of becoming stranded on a black screen.
- Send `/victory` boss completion to every connected co-op ally so each player can enter the shared victory and Beam Out result flow.
- Exclude full inventories before multiplayer equipment rolls, reroll if the selected inventory fills before grant, and play the pickup emote on the winning hero after a successful award.

### 2026-09-20

- Share collected DNA, health capsules, and power capsules across every connected co-op ally, including persistent DNA balances and synchronized active and reserve hero resources.
- Replace zero-value capsule pickup notices with the party's actual restored amount, and leave unneeded power capsules available after the Power full error.
- Remove Terrified gameplay state and modifier effects from defeated enemies across every co-op session while preserving the remaining duration on living splash targets.
- Move AI-controlled co-op allies and their summons through a teleporter when a human ally uses it so assistance resumes on the destination side.
- Reject campaign equipment pickups before their animation when inventory is full, keep the item available, and report the current capacity in game chat.
- Encode absent item prefixes and suffixes as null assets so pickup cards, Editor inventory, and subsequent mission loading do not resolve a bogus empty-string asset.
- Mark Remote play and the multiplayer-connections setting as still under development in the launcher.
- Extend Dendrone co-op spawn initialization to Healing Sprite, Beast Sentinel, Fire Tempest, Sentry Drone, and Plasma Sentinel summons, and send Sentry Drone's dungeon-entry spawn to allies.
- Replicate companion follow and attack starts, Dendrone respawns, movement-canceled channel effects, orb pickup presentation, and timed-area or Shockwave failure cleanup to co-op allies.
- Change map bosses from the ordinary equipment-drop chance to one guaranteed equipment drop per kill.
- Resume charging enemies after interrupted cooldown waits instead of leaving their attack action stalled.
- Correct Shade Drifter charge damage from projectile classification to its authored IgnorePlayerCount descriptor.
- Add co-op Omicron and Gravitic Confiner encounters with a shared one-or-two-per-map budget, channeled cage damage, ally-rescue controls, and cleanup when the captor is disabled or no rescuer remains.
- Remove Tree of Life from every connected co-op client if a healing tick fails, rather than leaving its animation running after the server stops the effect.
- Remove an aborting co-op player's party slot, heroes, and summons from teammates' clients while preserving the remaining party's mission, including when the host leaves.
- Keep ranged /ai firing between evasive moves instead of repeatedly extending dodges, and stop special-ability cooldowns from delaying basic attacks.
- Initialize Dendrones with their actual owner's player slot, explicit position, and stopped movement on co-op spawn and rejoin.
- Fix Tree of Life stopping on the second co-op player's healing tick, heal nearby allied squads and companions, and synchronize their health using the correct owning player.
- Let ranged heroes in /ai sidestep approaching projectiles and reposition between attacks while respecting movement restrictions and attack cooldowns.
- Replay teammates' current loading status when rejoining co-op so an already-ready ally does not remain at 0% in the reconnecting client.
- Keep unlocked teleporters usable by every co-op member after allies cross or backtrack, and synchronize pad activation and teleport presentation across clients.
- Add /ai for multiplayer co-op and PvP to assist allies with basic attacks, occasional abilities, and nearby following; movement, /follow, or another /ai returns control to the player.
- Preserve Continue and the shared co-op mission when a defeated player returns to ship or exits while an ally's squad is still alive, including when the departing player is the host.
- Change /recap from restoring only the caller's reserves to resurrecting fallen heroes across the connected co-op party, restoring control and synchronizing revived allies on every client.
- Send equipment pickup interaction data to teammates so dropped items respond to clicks on every client.
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
