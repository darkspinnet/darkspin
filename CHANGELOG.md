# Changelog

### 2026-09-25

- Show positive fractional hits as at least 1 damage in combat text for normal and critical hits while preserving their actual health reduction.
- Remove Elite and authored affix modifiers after an enemy's death state so their hand and status effects do not remain on the corpse.
- Preserve Sync Snapshot movement incidents at detection time across capture cooldowns, retain timestamped client/server movement history, detect persistent small offsets, and distinguish the triggering object's divergence from unrelated findings.
- Show killing blows at their full resolved damage instead of the enemy's remaining health, preventing fractional-health kills from displaying a zero-damage critical hit.
- Upgrade Sync Snapshot captures with retained incidents, connection-scoped analysis, explicit client object mappings, paired trigger and aftermath keyframes, gameplay decision history, transport and worker backlog state, broader drift detection, and completeness-limited confidence.
- Add `/stat` diagnostics that compare authoritative current-hero health and power against Fang's latest client-received resource values, including object identity and deltas for spotting desyncs.
- Extend campaign loot protection across solo and multiplayer with party winner rotation, shared equipment and catalyst dry-streak relief, catalyst type and rarity rotation, and durable per-account limited-edition boss pity that cycles compatible bases.
- Apply category rotation and rarity pity to multiplayer equipment when the roll winner receives it, using that player's activated heroes for compatible weapons while retaining one party-neutral mob-drop opportunity.
- Count only unequipped owned items against inventory capacity, matching the Arsenal counter so equipped hero gear no longer prevents loot pickup.
- Give Shade Drifters the authored transparent ghostform shader already used by Ghostly Trackers, including every difficulty and captain variant.
- Let Charging Grendels pursue into headbutt range without cancelling their melee sub-action and restarting it every recovery cycle.
- Restore mission 4-3's authored Cryos geysers with staggered warning and eruption cycles, damaging heroes caught over an erupting lava crack.
- Clip Ghostly Tracker charges against their actual through-hero travel segment, preventing distant navigation geometry from cancelling or prematurely shortening the charge.
- Preserve Lightning Juggernaut death-detonation damage across its distance falloff instead of rejecting fractional falloff results at the integer damage-selection boundary.
- Compose Nocturna tree variants per placement, deduplicating Nightmare Vines with their root clusters while preserving normal trees where most authored layouts use one.
- Restore Lightning Juggernauts to their normal movement speed after completing a charge instead of leaving the temporary charge-speed state active.
- Compose mission 1-4's smart-object scenery as a deduplicated union, restoring missing roots and environmental models without overlapping repeated markers.
- Aim Phantom Charge at Arakna's cursor instead of tracking a selected enemy, damage and silence every enemy crossed, then remove its shader and release the charging pose.
- Restore Arakna's five orbiting trapped-soul slots, fill them as Soul Ravager gains kill stacks, and empty them when Phantom Burst spends those souls.
- Resolve Phantom Burst hits along each radial projectile's full path and play its authored impact effect when a projectile reaches open ground, while retaining the six-to-twelve-shot soul scaling and four-hit-per-enemy cap.
- Aim Entangling Rush at Arborus's cursor position while preserving direct-hit damage and the authored area root when enemies occupy the destination.
- Choose requested `/drop` equipment categories uniformly from squad heroes with an eligible base item, preventing valid hero weapons from being crowded out by incompatible rolls.
- Keep Voltroid Charge Friend pursuit locked to its selected ally until arrival, preventing paired Voltroids from repeatedly exchanging client locomotion positions.
- Remove Meditron's Sentry Drone during the outgoing hero's departure instead of the next hero's arrival, preventing orphaned drone visuals from accumulating across squad switches.
- Preserve committed enemy spawn packets when an optional companion attack cannot start, preventing active enemies from becoming invisible, and allow presentation-suppressed projectiles to use their authored zero release delay.
- Keep Reconstruct's looping target presentation owned by its removable attached effect instead of recreating untracked visual and audio loops on every healing tick.
- Stop Pouncing Stalkers' spent self-resurrection ring from being recreated as an untracked effect, so it disappears after defeat.
- Restore Pouncing Stalkers' health, collision, and targetability in a stable order before playing their resurrection animation, while invalidating unfinished pre-death attacks.
- Interrupt Laser Tank attacks and remove their active beams immediately when the enemy is knocked back.
- Keep mission 4-1's Obelisk and smart-object scenery on the same authored layout, removing structures from conflicting variants that have no matching navigation collision.
- Normalize restored squad resources against each hero's current maxima so an over-cap reserve hero cannot block health-capsule pickups and movement.
- Damage heroes who walk through mission 4-1's molten-metal pool, including each independently controlled multiplayer hero.
- Add solo campaign loot bags that raise equipment chances after mob dry streaks, progressively weight overdue higher rarities, and rotate successful drops through compatible categories without replacement while leaving multiplayer and developer drops unchanged.
- Restrict generated weapon loot to the selected hero's authored family, preventing heroes that share a class and genesis type from receiving each other's weapon versions.
- Deliver Cryos lava contact damage presentation packets to the moving player and nearby peers instead of losing them to a shadowed response collection.
- Present unassigned squad records as campaign squads so newly unlocked squad slots resolve correctly instead of showing `undefined` and crashing the Arsenal.
- Match Sync Snapshot client receives across the complete shared emission window while excluding capture-edge traffic and RakNet heartbeats, so aligned-clock skew no longer reports delivered payloads as missing.
- Let Voltroids attack players between ally-recharge attempts, and immediately when no eligible ally is nearby, instead of repeatedly waiting on support behavior.
- Let Sinkhole enemies finish their gravity-well recovery animation before starting another attack.
- Give developer-spawned Mutation Agents a visible level-appropriate body and their authored passive effect instead of an invisible generic melee fallback.
- Remove chance-based Mutation Agents from natural campaign populations while retaining explicit developer spawning.

### 2026-09-24

- Play Orcus's authored melee animation when his basic attack deals damage.
- Make each Protoplasm apply its growth stacks to itself so its size, health, and damage visibly increase over time.
- Detach Grappling Pulsar's pull beam from its target when the effect expires or the casting enemy dies.
- Restore mission 3-2's authored lava-crack cave layout and apply contact damage when heroes move onto an active crack.
- Apply the Dimensionist Slow Shield debuff while its targeted hero is inside the moving time bubble, including authoritative movement-speed reduction.
- Aim Zetawatt Beam damage at its authored cursor position and ignore navigation-height offsets when checking enemies along the beam.
- Accept the build-103 `/taunt` action and play its shared hero taunt animation for the local player and multiplayer allies.
- Choose mission equipment loot by available slot before selecting a compatible base item, giving weapons the same category chance as other equipment.
- Give captain Elite and authored affix modifiers a valid lifecycle start time so the client retains and displays their buffs.
- Keep empty PvP squads unavailable until the player assigns a hero, preventing the client from crashing when the second squad is selected in the Arsenal.
- Keep mission 1-1 security portals visually dormant while enemies prevent their use, including when a new encounter spawns beside a portal.
- Keep NPC positions synchronized during direct pursuit fallback so enemies remain damageable when an authored navigation route cannot be projected.
- Let melee basic attacks pursue enemies near authored navigation edges instead of repeatedly rejecting the attack when the enemy position falls outside the normal movement projection range.
- Reconcile an idle returning enemy from the player's clicked position when the client omits its target position, preventing the enemy from snapping toward a stale origin and becoming unhittable.
- Apply Orcus's disease breath and ground-slam pulses through their active attack generation so their damage, disease status, and ground-slam thorn-spike effects are no longer discarded after the cast animation.
- Restore Orcus's Consume sequence so he eats nearby summoned minions with the authored animation, removes them from the encounter, and heals for each one consumed.
- Add cross-platform `-headless` Darkspinner presentation that opens the launcher in the default browser, serves it securely from `/launcher/` on the configured game port, and hands that listener from startup progress to the shared HTTP/Blaze server without requiring a Wails window.
- Add a `-headless`-only system tray menu for reopening the browser launcher or shutting down Darkspinner cleanly.

### 2026-09-23

- Register `/drop` with Fang's chat-command transport so `/drop create` reaches the server instead of being rejected by the native client.
- Disable experimental borderless fullscreen by default, expose it only as a development-build opt-in, and initialize new client profiles in windowed mode without overwriting existing display preferences.
- Disable WebKitGTK's DMA-BUF renderer by default in Wayland sessions to prevent the Linux launcher from exiting with a display protocol error while preserving explicit environment overrides.

### 2026-09-22

- Disable Shade Drifter collision during its charge so it can complete the authored pass-through movement, then restore collision afterward.
- Reject overlapping melee basic requests instead of acknowledging hits the server did not execute, preventing false health and power feedback.
- Let Raytheoid piercing lasers continue through players to their full range instead of ending at the selected target.
- Keep Botanical Tunnelers visible to their authored burrow animation while preserving server-side intangibility and their emerge attack.
- Accept post-mission Continue requests using the active squad instead of confusing the client selection token with a squad database ID.
- Present Magnos's Kinetic Wave effect on the caster when the ability begins.
- Keep mission 2-2 scenery and enemy placements on its canonical authored smart-object layout, restoring missing trees and removing conflicting models.
- Show captains' packaged Spiky elite affix using its valid aura modifier asset.
- Replace raw Spore and Darkspore audio registry literals with packed string, resource, reference, and usage indexes, reducing the embedded registry from about 6 MiB to 1.49 MiB without compression while preserving searchable conversion metadata.
- Change Darkrun-generated audio aliases, including registry-backed names, from the `ds_` prefix to a trailing `~` so inferred filenames are immediately distinguishable from authored names.
- Show the Darkrun build version at the start of root and subcommand help output, including bare general invocation.
- Decode structurally verified Spore XAS0 resources with their channel-interleaved frame layout, including shortened final frames, instead of misreading them as garbled XAS1 audio or preserving them as raw SNR files.
- Consolidate directly suffixed indexes, `_vN` versions, numbered variants, and numbered loop families into one normalized folder and family DSE while preserving each WAV definition and its reconstruction metadata.
- Reduce successful Darkrun conversion output to the elapsed time without repeating source and destination paths.
- Treat `darkrun convert <source> .` like an omitted destination so conversion writes the default local `.ds` directory or package.
- Give every retail Spore WAV a pinned human-readable `~` alias without runtime lookup, remove hash identity suffixes from aliased files, and annotate DSE files with searchable audio-event, animation, package, and resource references for reconstruction.
- Trace Darkspore audio events through shipped animation, noun, level, UI, effect, and pre-baked resources, pin exact names and resource-owner contexts into searchable conversion metadata, and replace ambiguous unresolved audio trees with resource-role and event-identity folders.

### 2026-09-21

- Replace Darkrun's external vgmstream dependency with native Go decoding and the Darkspin-maintained MP3 module for EA PCM16BE, XAS1, and MPEG-1/MPEG-2 EALayer3 v1 audio while continuing to encode edited WAVs as EA PCM16BE.
- Remove Draining Simian leech visuals from heroes when the draining enemy dies, even if shared effect-slot bookkeeping was cleared first.
- Anchor the Lightning Juggernaut's delayed death explosion to its rendered corpse and scale its damage from strongest nearby to weakest at the blast edge.
- Give each Space Barracuda an independent blink destination based on its own position, with a new direction on subsequent teleports.
- Aim ranged basic attacks at the cursor instead of redirecting them to a nearby enemy.
- Show Thunderstorm's cooldown on the ability HUD when its projectile storm begins.
- Initialize captain agent state before applying the Elite status and attach packaged affix modifiers to named population captains.
- Restore floating damage numbers for regular, critical, and killing hits against enemies, including co-op ally projections.
- Give converted audio streams deterministic `~` aliases derived from event, inherited parent, loop, registry, and shared-reference context; organize effects into family folders such as `effect/sfx` and `effect/scom`; annotate each WAV's DSE with searchable source keys, pointer roles, tags, and every AudioProps reference; consolidate numbered WAV derivatives with their event property list in one reconstructable DSE; and accelerate conversion with parallel WAV decoding and direct package streaming while reporting elapsed time.
- Persist each Crogenitor's creation timestamp, show it on launcher profile cards, and record the latest successful remote connection date beside its server address.
- Record the last local launch for each Crogenitor and show profile level and last-played date in local, Detached, and Remote selectors.
- Cache remote Crogenitor and server metadata locally, refresh servers in the background every two days, and refresh profile progress when a launched remote game exits.
- Move Botanical Tunnelers toward their target with the underground dirt-trail animation active, then emerge with their poison attack and wait through the authored cooldown before burrowing again.
- Restore Goliath's Shockwave to its full authored 4-metre reach and 6-metre hit arc, and let Zetawatt Beam pierce every enemy along its 35-metre path regardless of aggro target.
- Use Nightmare Vines' authored dead graphics state, retain their destroyed tree remnants, and avoid overlaying generic creature-death and Zelem explosion effects.
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
