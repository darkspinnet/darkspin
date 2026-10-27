# Changelog

### 2026-10-26


- #134 Use primary melee reach for Pouncing Stalker's selected target instead of the smaller additional-target arc, preventing captains from repeatedly swiping without reaching the hero.
- #134 Restore temporary charge movement state when Terrifying Curse interrupts an enemy, and retire interrupted charge follow-ups and Wraith Drifter continuations with their original action generation.
- #133 Preserve equipped movement and attack-speed bonuses when temporary slows apply or expire, and include equipment movement bonuses at mission entry.
- #133 Restore Celestial Comet's build-103 knockback distance, speed and recovery through the existing navigation-clipped forced-movement path.
- #133 Enable Necrotic Leech and its ranked variants to use their authored combat strafing between attacks.
- #131 Correct hero profiles from duplicated core-stat totals to build 103's base-plus-bonus format without increasing combat health.
- #131 Show equipped secondary stats in hero profiles using native whole-percent units and the complete supported stat-token set.
- #131 Prevent consecutive replay maps when another unlocked destination is available, preserving retained offers and first-run progression.
- #131 Prefer unused enemies in replay roster draws, retaining repeated slots only when the source pool lacks alternatives.
- #130 Gate optional Mutation Agent opening waves and barriers on completed campaign progression, preserving scripted initial-mission and boss-arena encounters.
- #130 Reply to queued basic-attack predictions replaced by an accepted ability instead of silently discarding their response token.
- #129 Defer portal contact polling during hero deployment so a portal cannot relocate the incoming hero before a delayed switch arrival places it back at the old position.
- #129 Restore the authored immobilization during enemy sleep effects and avoid repeated position corrections for held movement at the sleeping hero's stopped pose.
- #129 Stop held movement polls from replaying position corrections during tunnel traversal; the authored approach and exit walks remain unresolved.
- #129 Align Stinger circling movement with the server's lateral destination and preserve facing toward the hero using the build-103 movement contract.
- #129 Place Gravitic Regulator and Stabilizer DNA beyond their retained collision footprint, with terrain projection and a safe fallback at the hero when navigation is unavailable.
- Save completed unranked PvP match results and lifetime win/loss, kill/death, fight-time, damage and healing statistics for both players, with atomic profile updates, duplicate protection and save retries.
- #128 Restore Enrage and Tree of Life healing in PvP through their normal runtimes, with actual healing totals and round cleanup.
- #128 Restore Arboreal Might squad support and shared Celestial Comet, Time Lapse, melee-combo and projectile delivery paths in PvP instead of reducing these executions to immediate single-target hits.
- #128 Reply to canceled or replaced pending basic-attack requests so releasing held attacks does not leave the client unable to cast another ability.
- #128 Restore best-of-three PvP rounds with native victory/defeat overlays, a five-second pause, squad reselection and a fresh prep-area countdown between fights.
- #128 Replace fabricated PvP round scores, one-second duration and zero damage totals with recorded rounds, fight time and effective damage, retaining both players in completed results.
- #128 Restore build 103's native PvP prep-area countdown by sending its timer before GameStart and aligning combat entry with the authored animation.
- #128 Add a native no-party 1v1 scenario that independently queues both players and verifies shared Arena admission and combat readiness.
- #128 Exclude the dying hero from automatic reserve selection so a squad can continue fighting instead of becoming stuck with a dead deployed hero.
- #128 Preserve remaining Arena participants when the host opens results instead of aborting the opponent's pending results screen.
- #128 Add native PvP combat and knockout investigation scenarios with read-only squad, swap-cooldown and outcome snapshots for packet review.

### 2026-10-25

- #128 Drive the 1v1 scenario through native PvP / Unranked / 1v1 selection and Start, with verified party readiness and arena-owned world observations.
- #128 Fix PvP loading by sorting game attributes for build 103's binary-search lookup, accepting the native two-player Unranked 1v1 party, and honoring owned unlocked squads offered by the Arena selector.
- #128 Accept PvP scenario party invitations through native UI input instead of the server join fixture, wait for shared membership, and capture Navigation state and diagnostics when Arena acceptance stalls.
- #128 Start PvP scenarios from Navigation with established parties, using one shared two-player party for 1v1 and paired team parties for 2v2 instead of silently creating singleton parties from Arsenal.
- #128 Use pinned Blaze session IDs instead of account IDs for PvP scenario roster and host-session fields, matching production PvP setup.
- #128 Make campaign enemy targets adjustable from the fixed 15 per route section to 10–60 in Server Rules, preserving 15 by default and including the mission's effective target in bug reports and shared settings.
- #128 Prepare PvP scenarios with real owned playgroups and native user-group join notifications instead of unsolicited create/join callbacks, and identify which ownership field changes when a scenario loses its match.
- #128 Include Arena roster, squad-selection and loading handshake state in sync snapshots, and retain squad-accept rejection reasons beyond the ordinary log tail.
- Expand bug reports with rolling build-103 attack, movement-intent and camera/hero state, mouse/focus events, and explicit sampling gaps to investigate transient client/server desync.
- #127 Restore AcFreeze's build-103 Frozen attribute, attached ice block and ice-breaking effect, with cleanup on expiry, cleanse, death and hero changes.
- #126 Persist DNA pickups outside the global gameplay lock so account saves cannot block movement, attacks and NPC callbacks, while preserving pickup reservations and co-op grants.
- #126 Route targeted pursuit toward a walkable contact within attack range when an enemy's center lies inside a collision blocker instead of repeatedly rejecting the pursuit goal.
- Improve bug-report evidence by allowing five seconds for atomic client snapshot publication and attaching rate-limited, trace-labeled stack profiles for server dispatch stalls over 250 ms.
- #125 Replicate the client-authored deployment movement hold during hero arrival and switching, and avoid repeated position corrections for held movement at the locked pose.
- #125 Sample Ride the Lightning's admission time after acquiring the simulation lock to prevent stale timestamps from rejecting the cast with movement time regressed.
- Reduce navigation-footprint startup reads from the full asset catalog to noun names and extend the shared simulation-content load deadline from 10 to 60 seconds.
- #124 Fix black story movies after Beam Out by selecting the authored gameplay-return camera and fade-in before playback, and remove the client movie-material override.
- #124 Add a mission 1-2 completion cinematic scenario with native playback observations, screenshots and blank-screen diagnostics before ship voting.
- #124 Match the cinematic scenario's completion check to developer victory and expose screenshot pixel metrics to Lua so sustained black movie frames are detected.
- #123 Change replay missions from fixed destinations to shared maps unlocked by every player, with eligible randomized creature rosters, cross-pool duplicates, map-and-difficulty captains, and retained previews and resumes.
- #123 Preserve campaign enemy preview slots in selected roster order, including repeated nouns, instead of sorting and deduplicating their portraits.
- #122 Queue early ground-targeted and held basic attacks with their latest native response token instead of leaving them blocked behind unanswered input or older server-generated repeats.
- #122 Keep stationary basic attack animation offsets from accumulating into permanent hero movement.
- #122 Keep Sage unavailable until his authored tutorial introduction and preserve owned starter appearances and resources when he unlocks.
- #122 Stop Invincitron pursuit while its shield is active and resume pursuit when the shield ends.
- #122 Preserve authored portal rotations in mission 1-1 on initial spawn, checkpoint restoration and rejoin instead of resetting them to zero.
- #120 Defer early targeted basic attack repeats until the current attack releases instead of dropping their response and leaving the client blocked on its prediction timeout.
- #118 Use completed campaign progression for Mutation Agent eligibility, restoring agents and follow-up horde waves in earlier missions after the configured unlock (2-3 by default).
- #118 Retract horde fences through the client's door-open state instead of deleting them immediately, and restore completed fences open and nonblocking on rejoin.
- #104 Restore painted Body display when entering from Equip in both creature editors by completing native skin and material initialization.
- #119 Restore Tork's Sprout projectile lob movement and turn Tork and Char toward their basic attack targets.
- #119 Hold Tork's native movement during Sleeping Cloud and release it on cleanup instead of repeatedly snapping him back when walking is attempted.
- #119 Keep Auto Turret and Attractor Bomb at their placement positions instead of moving them with their owner.
- #119 Preserve Savage's ally and Healing Sprite through unrelated rejected commands and send their removal packets during failure cleanup instead of leaving client ghosts.
- #119 Restore Lifeforce Siphon for Skar and Wraith and preserve Shadow Cloak's guaranteed critical on its triggering melee attack.
- #119 Preserve active Proximity Mine, Dimensional Rift, Virulent Vines and Arboreal Might through unrelated rejected-command recovery.
- #119 Replicate ordinary enemy pursuit slows from Strangling Briars and Time Bubble to client locomotion and restore movement when they expire.
- #119 Restore Pain Hounds' client-authored cooldown and power cost.
- #117 Restore Missile Barrage's client-authored nonvertical descent slope while preserving impact points instead of sending exactly vertical missile trajectories.
- #116 Preserve accepted basic attacks when repeat input or held-aim updates arrive instead of falsely rejecting them and canceling their hits and release callbacks.
- #115 Allow fully mitigated hits to complete without damage text or failed melee actions, preventing repeated recovery baselines from replaying hero warp-in effects.

### 2026-10-24

- #114 Close active horde barriers with the client-authored door state and enable collision instead of retaining their initially open, non-colliding placement.
- #112 Preserve Pain Hounds' client-authored 1.9-second death animation when their duration expires or their owner deactivates them instead of deleting them immediately.
- #112 Replicate the client-authored Immobilized attribute during enemy stuns and avoid redundant movement corrections while the stunned hero remains in place.
- #111 Use the Corruptor's authored stage-two reward class at final defeat instead of retaining stage one's zero-challenge, no-equipment drop settings.
- #111 Play the Corruptor's distinct first-stage blink-out death animation while preserving its final-stage death animation and correct rejoin presentation.
- #111 Restore the Corruptor's authored transition with an 11.2-second death animation, a portal-clear wait capped at 120 seconds, and a two-second exit delay before stage two.
- #111 Preserve the Corruptor's existing minions through its first-stage transition and retire only remaining transition portals before stage two.
- #111 Replicate the Corruptor's Shadow Charge speed increase to the native mover and restore it on arrival, interruption, or phase replacement to prevent position snapping.
- #110 Track obelisk approach movement on the server and complete the interaction on arrival instead of repeatedly returning pursuit responses from a stale hero position.
- #110 Delay the Corruptor's phase controller until its client-authored entrance and completion publication finish, preventing pursuit during the cutscene.
- #110 Replicate the client-authored Immobilized attribute for enemy roots and avoid redundant correction teleports while the hero remains in place.
- #110 Keep one committed campaign clock stream per session so duplicate backward updates cannot corrupt native cooldown timing, including Supernova.
- #110 Preserve earned cash-out rewards when inventory is full, allowing the bounded reward batch above capacity instead of leaving placeholder reward cards.
- #110 Preserve the running basic attack when repeated input arrives during its accepted cycle instead of sending rejections that reset its native animation.
- #106 Correct Grappling Pulsar's pull effects from duplicate persistent world orbs to client-authored victim and linked beam attachments, removing both when the pull ends or its actors are retired.
- #106 Restore the Corruptor's first-stage death animation before its second-stage introduction, and preserve the transport identity needed to resume its second-stage attacks.
- #106 Preserve Lightning Juggernaut movement during charge redirects instead of repeatedly resetting its native mover and snapping at arrival.
- #106 Reconcile a controlled hero's position after an accepted stop when navigation leaves a significant horizontal client/server mismatch.
- #108 Fix the launcher getting stuck on Update available after preparation finishes, allowing automatic updates and the Patch button to start the update instead of treating it as ongoing preparation.
- #104 Move editor save errors from the Accept hover tooltip to the native on-screen hint area, retaining error numbers and explanations with scrollable and automatic paging.
- #104 Apply imported creature fitting to separately rendered parts as well as the body, preserving eye attachment positions and proportions across native poses.
- #104 Evaluate Mod emote movement eligibility at the current time instead of rejecting a completed path from its stale movement snapshot, and include eligibility details in rejection logs.

### 2026-10-23

- #104 Name save error 28 as a core-body template mismatch, explain why leg removal can trigger it, and reuse the native Accept tooltip with wrapped text instead of attaching competing save details.
- #104 Forward `/emote` through the client chat command handler so Mod emote requests reach the existing server handler instead of being rejected as an invalid command.
- #104 Revert the parts palette bounds change that separated the frame from its cards and enlarged the grid outside the panel.
- #104 Restore native mode 0 for Equip and track Body selection separately, keeping weapon placement and the equipment catalogue independent of the private body-parts palette.
- #104 Rework Mod Body category headers into aligned native radio-button frames with square icons and centered paging arrows, removing the legacy selected-tab flourish and animated strip decoration.
- #103 Give the Mod Body tab native radio-button selection and replace its ineffective hover effects toggle with the painted model render groups, preserving native picking and manipulation state.
- #104 Distinguish the Mod Body tab from Equipment using an installed limb icon over a native button frame instead of duplicating Equipment artwork.
- #104 Start Test Creature playback automatically, give walking a native movement route, cycle manual emotes through multiple dances and battle roar, and route preview voice cues through the current mouth family.
- #104 Adapt the full Import Editor environment's terrain and central platform materials through private runtime model aliases, extending ground compatibility beyond the fallback backdrop.
- #103 Move Mod editor save diagnostics from the custom Flash panel to the native Accept tooltip, with mouse-wheel paging for additional errors.
- #104 Enlarge Mod Body part previews from a ten-row grid to four columns and five rows, paging remaining parts in batches of twenty.
- #104 Change Mod Body category tabs from the classic cyan strip to installed Darkspore equipment tab skins while retaining category symbols, tooltips, selection, and paging.
- #104 Preload the Mod Body catalogue during editor entry and change card creation from eager all-page construction to selected-page-first batches with gradual off-screen warming.
- #104 Rename the separate Arsenal scene action to Import Editor and restore its availability by adapting the correct render-model resource, with the original scene retained if floor adaptation is unsupported.
- #105 Replicate the client-authored CaptainModifier on captain spawn and rejoin using a dedicated permanent handle, preserving existing calculated stats and scale.
- #104 Replace embedded palette assets with direct loading of installed native layouts and Fang adjustments to the loaded window instances.
- #104 Add Test Creature and Return to Body controls to the imported Mod editor, retain native skin baking, and enable its idle-animation update and message handling within scoped native calls.
- #104 Adapt the imported backdrop's lit paving and ground materials to its textured unlit renderer.
- #104 Suppress the native lobby level's static-model rendering and optional pedestal models in the imported creature-editor scene while retaining its creature, backdrop, and editor controls.
- #104 Restore native Equip and Detail tab visibility in the imported creature-editor scene and restore the Detail palette root when leaving Mod Body mode.
- #104 Use installed native UI resources for the parts palette, with larger four-column previews and native paging with up to forty parts per page.
- #104 Add Body category tooltips with recovered native labels and readable fallback titles for parts whose authored names remain internal placeholders.
- #104 Restore the native creature-parts palette in Mod Body mode with compatible creature cards and native part dragging, attachment, and symmetry.
- #104 Route core foot placement in Mod Body mode from the stock hero attachment controller to the general creature manipulator, retaining native symmetry and edit history.
- #104 Keep the imported editor environment across owned-hero switches and isolate its Body view from native room geometry and combat stats while retaining equipment, detail, paint, preview, and save controls.
- #104 Keep the Mod Arsenal Creature Editor button enabled for the last eligible creature after its hover menu closes, while rechecking ownership and creature identity before opening it.
- #104 Add a Mod Arsenal Creature Editor button that selects the original creature-editor backdrop and lighting through private on-demand resource aliases while retaining the selected hero's body controls and save flow.
- #103 Expand Mod Creature Edit save diagnostics from a single native message to a paged panel covering all 128 validation bits, with shared creature-editor error names and trace details for unmapped failures.
- #103 Give native part hover and selection highlights a cool blue color in the Mod Body tab to distinguish them from cyan bones and white joints.
- #103 Increase Mod Body overlay contrast from muted pink to bright cyan, with white highlighted vertebrae and stronger hover opacity while preserving native fading.
- #103 Expand Mod Creature Edit from bound imports to all owned heroes, including stock hero appearance-save compatibility without requiring import bindings.
- #103 Allow native body and bone picking in the Mod Body tab by clearing the player editor's exclusion filters while that tab is active.
- #103 Give the Mod Body tab fixed drawing bounds and a centered wrench so its background and click area align with the native editor tabs.
- #103 Separate the Mod Body tab from equipment mode and synchronize native tab selection when switching between editor controls.
- #103 Correct creation-cache index-size parsing so Mod scans do not treat the following header word as part of the size, and identify the failing package in scan errors.
- #103 Match the Launch Modded button to the Mod tab's amber accent instead of the default green.
- #103 Rename the Mod Arsenal action to Creature Edit and match its spacing to the native menu rows.
- #103 Add a Mod-only Creature Editor action to bound heroes' Arsenal menus, enabling retained body and limb controls through the selected hero's existing editor and save flow.
- #102 Restore authored fixed catalyst bonuses instead of scaling percentage effects by item level, including Attack Speed from 73% to 5% for a common level-73 catalyst.
- #102 Include equipped-part secondary attributes in hero profile More Stats without double counting saved editor values.
- #102 Cancel accepted pursuits and held basic attacks when sleep or another movement-stopping control effect takes hold.
- #102 Restore Charged Strike's pursuing melee definition instead of classifying its target taunt as the entire ability.
- #102 Retire Voltroid charge beams when either endpoint dies, including retained multiplayer owners.
- #102 Track Kinetic Wave's push effect and remove it on landing cleanup, target death and session shutdown.
- #102 Preserve Ecostabilizer damaged models after destruction and reconnect, and restore Dysfunctional Ecostabilizer's authored warning and periodic freezing wave.
- #102 Preserve authored security teleporter rotations and remove duplicate retirement packets during initial presentation.

### 2026-10-22

- Apply bound Mod creature sizing to final model transforms instead of animation rig inputs so cached and animated models use the same visual enlargement.
- Reduce the automatic enlargement cap for creatures bound to Mod heroes from 2x to 1.5x native visual size, preserving proportions without saving per-binding multipliers.
- Expand Mod bindings from heroes to categorized character nouns, with automatic fitting from shared noun size data and imported rest bounds instead of saved per-binding scale multipliers.
- Add `/emote` happy, laugh, scared, swoon, roar, backflip, flex and yawn for bound creatures in Mod missions, with native animation playback and multiplayer presentation.
- Route Mod creature-editor music from Spore's general sound groups through native music groups using a private in-memory event, retaining native playback handles and exit cleanup.
- Extend bound Mod creatures' voices to combat, reactions, idles and taunts using mouth-matched sound families and native animation cue timing.
- Use bound creature portraits in Mod squad and HUD image controls and template profiles while retaining Darkhero names.
- Keep Mod Crogenitor details visible across tab switches and disable dependent actions while refreshing validation in the background.
- Match the top-left Dark Spin logo to each tab's accent, including blue for Remote and purple for Detached alongside Mod's amber.
- Add per-hero Mod size targets with uniform fitting to assembled rest bounds, preserving imported proportions and native presentation scales.
- Play mouth-matched vocalizations with bound Mod creatures' dance animations and stop their audio on movement, animation interruption or model replacement.
- Expand editor part-scale limits for explicitly bound Mod creatures so imported proportions remain unchanged, while retaining missing-part, finite-scale and other native validation checks.
- Load audio banks for imported Mod creatures so native mouth and footstep selection can resolve their sounds.
- Initialize missing material settings on bound Mod creatures with Blitz's native material settings while preserving paint and existing materials.
- Keep edited creatures bound to their Mod heroes after native editor saves, and retain their saved appearances across Mod relaunches instead of restoring the original import.
- Widen the Mod Crogenitor binding editor, compact creature and hero selectors into one row, trim introductory copy, and replace binding editing with delete-and-recreate.
- Use explicitly bound creatures as their Mod appearance baselines, preserving native validation checks while allowing their bodies to differ from the stock hero template.
- Remove Mod status messages beneath the Crogenitor selector to match the standard profile layout.
- Stop Mod settings from automatically selecting and highlighting the path when opened.
- Identify modded launcher and in-game bug reports with Mod filename prefixes, explicit session metadata, versioned mod component names, and the isolated Mod server logs.
- #101 Restore Shockwave's authored melee strike, hit radius and impact effect so casts can apply damage and start their cooldown.
- #101 Correct Arc Weld's cast range to include both actors' footprints, restore its authored chain radius and allow hostile targets attacking other heroes.
- #101 Remove Mutation Agent's duplicate model effect at object creation, leaving activation and rejoin to publish its authored persistent effects.
- Replace converted package snapshots with Mod-only Fang streams that translate DBBF indexes in memory and read original resource payloads on demand.
- Give Mod a distinct amber primary color across its launcher view, creation wizard, settings, and dropdowns.
- Change converted package indexes from per-entry instance-high words to the shared zero word required, rebuilding cached mounts and retrying imported appearances.
- Convert DBBF caches to launcher-owned DBPF snapshots for Mod launches and refresh creature appearance revisions so rejected imports are requested again.
- Change Mod from a persistent binding panel to the standard Crogenitor view, with bindings managed through Edit Crogenitor and the creation wizard.
- Add experimental Mod Crogenitors with discovery, creature and hero portraits, editable profile bindings, native resource loading attempts, and isolated mod.db launches.
- #100 Expose all Mutation Agent horde settings in the launcher's Server Rules dialog, including validation, saving, reloading and GitHub feedback export.
- #100 Keep horde followups alive through player disconnects, preserve their delay, and deliver Mutation Agent spawns before their effects and movement.
- #100 Add server configuration for Mutation Agent horde eligibility, wave count, spawn wave, delay and encounter completion, applied to new missions.
- #100 Recover stale Mutation Agent life presentation through targeted baseline recreation and prevent acknowledged feedback from replaying structural packets in queued replacements.
- #100 Restore combat music input from current per-hero enemy targeting counts, clearing stale counts and refreshing through busy combat, hero switching and reconnects.
- #100 Replace Mutation Agents' instant nearby promotion with authored ally selection, movement, timed beams and target-owned elite transformations.
- #100 Restore Mutation Agent aura timing, hero and companion targeting, transformation immunities and immediate death deletion, with reliable damage retries and co-op feedback.
- #100 Preserve Mutation Agent progress and accepted callbacks through resurrection, rejoin and safe checkpoint recovery; checkpoint format advances from 10 to 11 and older checkpoints cannot resume.
- #100 Correct Krel's passive stun activation tracking and preserve Poison Nova's prior source snapshot when a refresh fails.
- #100 Keep Overdrive meters and combat attributes current through expiry, co-op ownership changes and reconnects.
- #100 Restore authored chance, animation and timing for supported NPC first-aggro reactions without rerolling on retries.
- #100 Add survival, health-obelisk avoidance and Quest Crystal objectives, including shared quest pickups, pursuit and reliable reconnect presentation.
- #100 Preserve objective event subscriptions, player counters and pending quest drops through checkpoint recovery; checkpoint format advances from 9 to 10 and older checkpoints cannot resume.
- #100 Correct boss entrance timing, camera focus and facing, delay SpawnEater's entrance animation as authored, and restore elapsed entrance poses on rejoin without replaying expired camera requests.
- #100 Preserve introduction progress through safe checkpoint restart; checkpoint format advances from 8 to 9 and older checkpoints cannot resume.
- #100 Restore Scaldron's second entrance protection and nearby NPC and companion holds, preserving the original deadline, ordered co-op presentation and phase recovery through disconnects and preparation failures.
- #100 Restore Citadel charging-target animation resets and cinematic interruption rules while preserving transfers whose initial hit was already accepted.
- #99 Preserve channel effect creation and removal order through co-op reconnects, hold visual slots until removal delivery, and prevent stale drain callbacks from changing replacement actions.
- #99 Preserve initial environmental explosion feedback and corpse deadlines through co-op ownership changes, and settle accepted blasts before world reset or shutdown without repeating damage or rewards.
- #99 Correct Savage Rage's fractional and critical healing, preserve its accepted initial healing feedback, and use continuous healing ranges for Strangling Briars and Sporogenesis.
- #99 Correct Time Lapse's damage-history cutoff and prevent late Plasma Column or healing-channel failures from restoring stale mana and cooldown state.
- #99 Preserve accepted healing feedback when statistics recording fails, and exclude destructible fixtures from NPC damage objectives.
- #99 Restore Plasma Wreath's damage-triggered retaliation, creature targeting and ranked effects, preserve accepted casts through recovery, and hold visual slots until removal is delivered.
- #99 Preserve supported objective counters through checkpoint rejoin and apply campaign difficulty and stage scaling; checkpoint format advances from 7 to 8 and older checkpoints cannot resume.
- #99 Correct Voodoo Charm's dead-contact admission and healing feedback, preserve Savage Ally resurrection ownership through co-op reconnects, and capture Savage Rage healing attributes at activation.
- #99 Resume ordinary NPC entrances after co-op ownership changes, preserve Quantum Blink's random facing, and correct local movement rejected by sleep or stun.
- #99 Apply tutorial and world unlock callbacks to eligible party members and restore the robot entrance's authored timing and animation.
- #99 Restore Fire Elemental and Plasma Sentinel pet death animations and Drainer's delayed overload explosion with ordered damage, corpse cleanup and rejoin handling.
- #99 Restore Strangling Briars' ranked allied healing, hostile roots and movement slow, including independent root expiry, cleansing and rejoin presentation.
- #99 Apply operative cage ranks, Cyber pulse timing and health or accumulated-damage escape rules while preserving committed co-op presentation.
- #99 Preserve Toxic Spores' original poison source and independent cloud-tail lifetime, and correct Expunge's remaining-damage budgets and modifier cleanup.
- #99 Preserve environmental disease and burn source snapshots and tick positions when an active co-op peer takes over, including disconnects before scheduling or publication.
- #99 Restore Sporogenesis' independent healing callbacks and retain pending heals through reconnect handover without extending their lifetime.
- #99 Give Omni Shield independent overlapping children with authored activation and expiry, hit-time payment and cleanse, retained protection and safe release responses.
- #99 Correct Dimensional Rift's banished-target admission and ranked cooldown, Time Lapse's current Dexterity input, and Plasma Column's tick count, timing and current source attributes.
- #99 Restore Poison Nova's upgraded refresh cue, Psistorm's fractional tick count and Sleeping Cloud's per-cloud sleep-attempt history.
- #99 Clear Rooting Plague roots on their own deadlines, cleansing and target death, and publish infection cleanup through switching, disconnects and co-op teardown.
- #99 Restore Voodoo Charm's authored mana-return effect on qualifying kills even when resources are full.
- #99 Correct Nightmare Vine root damage from fixed Spectral/Physical damage to the authored Elements/Energy range, radius and impact effect.
- #99 Restore Plasma Wreath's ranked orb counts, damage, defense bonus and upgraded haste lifetime, with co-op expiry and departure cleanup.
- #99 Allow Rooting Plague recasts to replace primary disease and roots while preserving existing spread infections and their original damage credit, and keep spread ticking on authored deadlines while infections remain.
- #99 Extend Voodoo Charm to eligible allied combat companions, restoring their own Overdrive health return on kills and preserving death and co-op ownership rules.
- #99 Preserve equipment affix modifier grants and ability improvements in the content database and catalog for inspection.
- #98 Restore Corruptor's final-stage boss loot eligibility after its stage-two noun transition.
- #98 Keep Invincitron's drone orbit following its living owner between combat actions instead of stopping when attack state clears.
- #98 Ignore deaths of summons outside the admitted horde wave so auxiliary drones cannot reject combat updates or alter wave completion.
- #99 Calculate NPC DNA drops from their class and active Elite challenge with the authored amount tuning instead of a fixed source amount.
- #99 Apply the recipient's co-op item-level floor to pickup and Cash Out rewards while preserving selected item identities and retry behavior.
- #99 Reject resolved affix combinations with no positive combined modifier during generation and stat calculation, including weapon fallback handling.
- #99 Preserve boss introduction deadlines through co-op ownership changes and restore the remaining cinematic hold on rejoin without restarting the entrance for other players.
- #99 Remove Rooting Plague's shorter upgraded root at its own expiry and clean up roots retained by Expunge when their target dies.
- #99 Apply the authored Elite reward challenge bonus to equipment and catalyst drops using the NPC's actual modifier affix.
- #99 Preserve authored audio triggers and their nested geometry in the content database for inspection, including separate noun definitions and marker overrides.
- #99 Add a read-only Lua audit command that reports bytecode integrity, dependency links, callback metadata and production hero definitions while keeping unverified gameplay behavior explicit.
- #99 Restore missing basic-attack burn, vulnerability, haste, weakness, silence and knockback callbacks with their authored stacking and expiry rules.
- #99 Restore Savage pet vulnerability and owner resurrection, including revival indicators, failed-revival cleanup and protection against premature squad defeat.
- #99 Correct passive rank and Overdrive bonuses, nonbasic-ability stun charging, companion counts and supported passive reactivation when entering Overdrive.
- #99 Restore Arc Weld chain hits, Rewind's allied hero cooldown reduction and Expunge's consumption of supported co-op damage-over-time effects.
- #99 Restore Quantum Blink position and animation on interruption, clear retained immunity and keep co-op hero positions consistent through cleanup and rejoin.
- #99 Initialize Titan's passive shield immediately on activation while preserving its delayed recharge after breaking.
- #99 Restore Flame Surge's piercing wave, authored collision volume, growing hit filter and contact effects.
- #99 Restore Sage's shared companion refill timing, distance recall, emergence and burrow, with consistent ownership and presentation through switching, Overdrive and co-op rejoin.
- #99 Regenerate Titan's depleted shield on its authored timer and retain its recharge deadline and presentation through reconnects.
- #99 Publish scheduled Overdrive updates to co-op peers after transport commit and start companion polling after the owner's presentation is sent.
- #99 Resolve 480 previously unlinked Lua dependencies using native resource identities while retaining explicit diagnostics for unresolved references.
- #99 Allow Krel's charged stun to trigger from qualifying child damage and start its retention window on the first qualifying hit.
- #99 Preserve Voodoo Charm's first accepted aura owner through overlapping providers, death, Overdrive entry and co-op disconnect/rejoin.
- #99 Extend Rewind to eligible allied combat companions while preserving accepted attack release timing and co-op ownership.
- #99 Let Expunge consume Rooting Plague's remaining disease damage while preserving its separate root, and credit co-op damage transitions to the original damage-over-time owner.
- #99 Preserve authored NPC introduction delays on scheduling failure and prevent stale boss activation or cleanup from affecting replacement actions.
- #99 Make Pummel's silence respect Unstoppable immunity while preserving its separate repeat-hit marker.
- #99 Restore Quantum Blink's initial hit plus five strikes, correct overlapping-target placement, and release immediately when no targets remain without the unused ending pause.
- #99 Give Rooting Plague immunity its authored seven-second lifetime across co-op owners and let Expunge consume Plasma Sentinel's remaining burn damage.

### 2026-10-21

- Restore Shielded affix absorption and visuals across NPC spawns, regenerate shields on their client-authored timer, and preserve them through DLS-1227's half-health invincibility transition.
- Restore CaptainModifier crowd-control resistance and multiplayer timing bonuses, plus Frenzied attack-speed and cooldown bonuses.
- Restore Tough's type-dependent HP bonus, Accurate critical rating, Ghostly and Reflective defense ratings, and Armored and Sloth Passive flat armor.
- Restore Spiky melee retaliation and one-time Persistent resurrection with native effects and protection against duplicate loot and premature encounter completion.
- Restore non-stacking enemy aura propagation and cleanup, including Frenzied, Ghostly, Reflective, Spiky, Shifted, Surefooted, Unstoppable and Persistent recipients.
- Restore Packfly Passive's ranked nearby-helper bonuses and effect tiers, replacing the fixed one-ally bonus with up to seven helpers within eight units.
- Restore the client-authored CaptainModifier and Deadly affix damage bonuses in the shared enemy damage calculation.
- Preserve authored captain names and affixes for /spawn, matching normal encounter spawning.
- Restore the client-backed CaptainModifier HP bonus across captain encounters, population spawns and /spawn, increasing first-run solo Illust from 105 to 300 HP without compounding on continue/rejoin.
- Replace the captain 1.4 spawn-scale override with the client-authored CaptainModifier BodyScale bonus, preserving it through continue/rejoin and combat growth effects.
- #96 Correct rooted heroes through native owner-aware locomotion and pose updates so local movement prediction cannot bypass the server's stop.
- #96 Stop Acid Shell's existing pursuit when it retracts into its shell and resume attacks after recovery.
- #96 Retire authored security teleport pads across campaign levels before creating server presentation anchors, preventing doubled pads.
- #96 Calculate Voltroid discharge damage from its current charge count instead of maximum charge, and keep its lasting charge glow from drawing a continuous ally beam.
- #96 Keep Citadel boss factory vents in their dead graphics state after damage and resume their authored fire trap instead of exploding and removing them.
- #96 Apply Citadel plasma pool burning effects and damage to hero pets, with cleanup on exit, death, switching and despawn.
- #97 Fix /scale previews from combat-attribute-only updates to native object-scale updates, preserving baseline-based sizing, reset and rejoin behavior without overriding combat growth attributes.
- Add /god to toggle testing invulnerability across hero switches, including enemy and lava damage, and record its state in bug reports.
- Apply the client-authored elite HP bonus to /spawn captains, matching normal captain encounters.
- Add /scale visual previews for nearby enemy NPCs with radius and noun filters, reset support, party-wide presentation and exact preview values in bug reports.
- Allow /spawn in normal campaign and tutorial missions, and flag command-spawned NPCs in bug reports for all party members, including after checkpoint restoration.
- #95 Recognize recently acquired mission items when dropping them, restoring ground drops instead of only removing their inventory cards.
- #95 Accept Charging Brute and Grappling Pulsar secondary-attack pursuits so server movement continues alongside client movement.
- #95 Apply Charging Brute's client-authored charge speed multiplier through both approaches and restore normal speed on completion, cancellation and blockage.
- #95 Keep Charging Brute and Pouncing Stalker server movement on the prepared footprint-aware stopping profile instead of advancing toward the target's center.
- #95 Correct lobbed cloud projectiles from client wall-clock timestamps to server timestamps, matching the native client's existing simulation-time conversion.

### 2026-10-20

- #94 Clear Arboreal Might's attached effect, modifier and body scale when action recovery cancels its expiry.
- #94 Restore the client-authored reaction reset after enemy teleport hits so heroes do not remain stuck in the teleported animation.
- #94 Restore Char's client-authored pursuit for out-of-range basic attacks and include actor footprints in targeted range admission.
- #94 Allow Jinx's client-authored ground-targeted basic attacks without automatically selecting a nearby enemy.
- #94 Keep Healing Sprite stationary through its authored sting, release the animation after one second and resume following its owner.
- #93 Use the client clock for Tork's lobbed basic-attack projectiles so their flight starts at the correct client time.
- #93 Restore Vex's client-authored basic-attack hit effect when imported Lua omits its preloaded asset reference.
- #93 Allow Seraph-XS's Trapper Grenade to use a supplied ground target instead of requiring or automatically selecting an enemy.
- #93 Restore Auto Turret's client-authored placement pursuit, casting animation and muzzle effect.
- #93 Restore Attractor Bomb's sustained taunt effect and remove it when the bomb detonates or is destroyed.
- #93 Persist the native editor save's quoted DNA cost with the creature update, rejecting insufficient funds and rolling back the charge if saving fails.
- #92 Accept straight NPC movement through intermediate navigation portals instead of treating every portal crossing as a blocked route, while retaining checks against bends and off-route projections.
- #92 Restore Vampiric Leapers' client-authored jump packets, approach and retreat animations, airborne hitbox movement, and landing-only attack damage across all ranks.
- #92 Replace rank-scaled movement catalyst bonuses with client-authored fixed percentages and preserve the ordinary epic catalyst's authored attack-speed effect.

### 2026-10-19

- #91 Send an authoritative pose and stop when enemies return home instead of only a movement goal, preventing unattackable enemies from remaining at their old client positions.
- #91 Start NPC corridor movement once and send subsequent waypoint changes as goal-only updates, preventing repeated native locomotion resets and correction jumps.
- #91 Preserve navigation corridor waypoints during Hypno Mantis's secondary melee chase instead of redirecting straight toward the hero.
- #91 Match Ragetusk's server and client charge stopping distances, restore its authored approach and landing stages, and clear its speed bonus when pursuit loses its target or becomes blocked.
- #91 Apply Shade Drifter's charge speed bonus as the client-authored multiplier instead of a flat speed increase, including existing movement bonuses.
- #91 Use representable float32 movement steps near NPC attack range instead of rounding to no movement and repeatedly stopping and restarting pursuit.
- #90 Restore Botanical Tunneler's client-authored above-ground attack animation, hit timing and cooldown instead of entering underground travel during each attack.
- #90 Synchronize Ragetusk's charge with the client-authored movement-speed multiplier and restore its normal speed before the follow-up attack and on failure.
- #90 Admit movement from the sampled authoritative position instead of an obstructed client prediction, allowing bounded reconciliation without repeated start-position corrections.
- #89 Retain a validated approach point when obstacle-contact terrain projection is obstructed, reducing rejected movement corrections while preserving collision and connectivity checks.
- #89 Expand camera snapshot diagnostics with native follow anchors, interpolation state and cached transforms, and report the latest completed snapshot's fields instead of an earlier capture.
- #88 Restore client-authored pursuit and footprint-aware range admission for Jinx's thrown basic attack, preventing repeated out-of-range failures while its animation plays.
- #88 Register Dendrone respawns for every enemy damage death path and route them to the summon owner's session, preserving the authored respawn delay in co-op.
- #88 Consume repeated triggers for already-admitted horde encounters instead of attempting duplicate admission and disconnecting the player on the next gameplay poll.
- #88 Change Proximity Mine's trigger range from 20 to the authored 3.5 units, restore its explosion effect, and include placement and arming time in its timeout.
- #88 Restore Web Transfusion's five area-drain pulses, damage-based healing, full channel timer, beam and spark effects, and authored ending animation with interruption cleanup.

### 2026-10-18

- Add Borderless Mode in the game's graphics options, mutually exclusive with Full Screen Mode and preserving the selected resolution.
- Add Taskbar Overlay controls
- Remove custom resolution handling, limit Borderless Mode to window decorations, and change Taskbar Overlay from always-on-top positioning to the Windows taskbar API.
- #86 Change Resonance's basic attack damage bonus from raw defense ratings to converted, capped dodge and resist chances, matching its client-authored tooltip.
- #80 Restore client-authored pursuit for ranged support bursts, keeping the server hero alongside the client before the ability fires.
- #80 Keep Rezzers retreating toward their chosen flee point instead of redirecting into the hero during Ghostly Bolt recovery.
- #80 Approach reachable collider edges for walking clicks deep inside obstacles instead of repeatedly rejecting and correcting the hero.
- #85 Restore Hardened Mantle's damage-triggered effects and stack display, and increase its Overdrive damage reduction from 4% to 10% per stack.
- #85 Restore Lumin's Chain Lightning from a single impact to its authored beam and two chained damage jumps.
- #85 Apply the strongest nearby friendly Charge Field bonus and increase its critical-rating contribution from 50% to 75% during the source's Overdrive.
- #85 Stop Nightmare Vine root damage after mission completion, preventing inactive-zone errors from disconnecting players on the mission-complete screen.
- #85 Handle the client's lobby leave request with its native response and member-left notification instead of returning command not found.
- #81 Restore Chrono Blink's native teleport message so the client hero moves with the server position.
- #81 Keep completed melee damage and destructible destruction synchronized when movement interrupts an attack before its hit packets are published.
- #81 Enable client-authored pursuit for ranged basic attacks, preventing Krel and other pursuing projectile users from repeatedly rejecting reachable targets as out of range.
- #81 Remove Enrage's modifier, visual effect, size increase and damage bonus when failed-action recovery cancels its expiry timer.
- #81 Remove Pain Hounds from clients when failed-action recovery retires their server objects, preventing stationary summons from lingering after their timers are canceled.
- #81 Restore Ride the Lightning's client-authored target-based landing and damage against destructibles instead of rejecting selected targets using an unrelated cursor position.
- #84 Restore the Corruptor's second-stage target after resurrection instead of aborting the transition on its retained threat entry, allowing queued encounter cleanup and stage-two publication to complete.
- #84 Enforce Grappling Pulsars' client-authored pull cooldown across initial shots and pursuit recovery, and select their fast melee attack for targets already within reach.
- #84 Keep Destructor modifiers while removing the generic Elite nameplate affix from their spawn and reconnect packets.
- #84 Populate build 103's actual interlevel cinematic field, restoring the authored Corruptor epilogue and following planet presentations.
- #82 Spread health-obelisk and enemy capsule batches across client-authored shuffled navigation-projected rings instead of landing every capsule at the same point.
- #82 Deliver queued NPC spawns before starting recovered actions, preventing Nashira's entrance setup from arriving before her object exists and leaving her moving during the reveal.
- #82 Retire authored teleporter duplicates when loading or rejoining mission 1-1, retaining one server-controlled pad at each of its seven security teleporter locations.
- #82 Restore Twinblaze's client-authored first and second firing animations after its warmup for all heroes using the shared ability.
- #82 Keep Exploder Scarab fuse chases on the client's pursuit path instead of overwriting intermediate waypoints with the hero's position.
- #82 Preserve Destructor equipment drops' displayed item type, rarity, level and affixes when collected instead of replacing them with a deferred pickup roll.
- #82 Synchronize Lightning Juggernaut charge speed with the client's multiplicative speed modifier and replicate the charge template's actor-avoidance toggle, restoring normal movement afterward.
- #82 Give Magnetic Master's push and pull their separate client-authored cooldown deadlines, allowing a ready push during pull recovery and preventing early pulls after a push across all variants.
- #82 Restore the client-authored fade-out, two-second transfer delay and fade-in for campaign tunnel teleporters, with input locked and damage immunity during transfer.
- #83 Give saved gameplay appearances revision-qualified identities so players with the same account-local hero ID no longer share the wrong multiplayer model; keep template appearances distinct by hero noun.
- #83 Send the full hero roster once per loading session and use sparse progress updates afterward, preventing repeated co-op roster refreshes from restarting hero asset loading.
- #83 Place mission-end rewards, XP and daily bonuses in the recipient's actual multiplayer slot instead of always player 1, and calculate reward rarity using that player's medals.
- #83 Drain queued co-op animation and world updates on ordinary gameplay requests instead of waiting for a separate poll, while preventing duplicate batch delivery within the same request.

### 2026-10-17

- #76 Preserve displayed enemy and captain equipment drops when awarding them instead of rerolling their item type, rarity, level and affixes after pickup.
- #77 Restore normal walking speed when Beast Charge or another hero charge is interrupted or fails, preventing Savage's charge speed from persisting after the ability ends.
- #78 Fix editor items incorrectly showing Out of item by reading the correct live creature model and retaining native availability checks when model inspection is inconclusive.
- #79 Keep squad lists and saved hero assignments in stable slot order instead of moving the active squad first, so selecting squads 1, 2 or 3 deploys the matching lineup.
- #74 Make first tutorial completion and tutorial skip finish at Crogenitor level 3 with Blitz, Sage and one hero activation credit, preserving later progression on repeated completion calls.
- #74 Change hero activation rewards from selected milestones to one per Crogenitor level after the three tutorial heroes, restoring missing credits for existing accounts on profile load.
- #75 Correct Overdrive's malformed active-state packet to restore native glow, sound and HELIX activation/ending cues, and share active-state changes with co-op peers while retaining the six-second base duration.
- #75 Synchronize mission HUD core attributes and weapon damage with equipped hero stats instead of fixed placeholder values, preserve them during recovery, and align profile/combat defense and critical ratings with the client's Dexterity and Mind contributions.
- #75 Resolve movement clicks onto the connected stair or ramp surface instead of rejecting goals carrying the hero's previous height, preventing repeated movement corrections while preserving horizontal tolerance and obstacle checks across maps.
- #75 Restore all Electric Striker variants' client-authored eight-hit melee bursts and recovery before retreating, restrict damage to their short forward melee arc instead of teleport activation range, and prevent repeated approach blinks from replacing the attack.
- #75 Restore the client-authored source-object and target-point fields for all Maser Tank laser shots, replacing detached world-position effects with beams emitted by the attacking creature.
- #75 Prevent placing the same physical item twice from the editor palette by checking the live model and mirrored attachments, persist equipped-item usage, and reject incomplete or invalid loadout saves instead of silently dropping equipment.
- #75 Exclude unpurchased Parts shop offers and sold buyback items from the editor's functional and detail palettes, and require owned items for creature equipment responses and calculations.
- #75 Display Parts shop purchase prices as Cost instead of Sell Value while retaining normal owned-item and buyback valuations.
- #75 Expire buyback items two hours after sale, persist their deadlines across restarts, and remove expired or legacy undated sold items on the next inventory load without reusing their IDs.

### 2026-10-16

- #73 Add a Parts tab to build 103's DNA Converter with rotating server stock, single-item selection, purchases and native tab transition effects, keeping inventory images and tooltip queries intact.
- #73 Add persistent server-side Parts shop stock across all five genesis types, rotating 22–30 non-weapon offers every two hours by default with a configurable 5% green chance per offer capped at five greens, weapon-level pricing and one purchase per offer.
- #73 Add Fang compatibility for server-priced Parts shop offers in build 103, aligning tooltips, selection totals and purchase charges while allowing exact-DNA purchases and preserving normal resale and buyback valuations.
- #72 Restore Warp Totems' client-authored charge/discharge cycle, nearby random teleports and spatial effect events; replace generic explosions with their collapse effect and retain destroyed models across reconnects.
- #72 Replace Sinkhole's repeated hero teleports with directional movement influence, restore client-authored well timing and effect cleanup across co-op peers, and preserve NPC projectile speed changes under gravity.
- #72 Restore Hover Bots' client-authored circling, idle wandering and linked-beam damage, remove links when either bot dies without duplicate hits, and spawn selected campaign Hover Bots with at least one partner as a server encounter rule.
- #70 Restore Overdrive's initiation animation, 50% damage reduction, 20% attack-speed bonus, client-authored 40% cooldown reduction and status immunity; preserve charge through its windup and prevent switching or transferring its buffs to another hero.
- #70 Publish Sentry Drone deletion during action recovery and runtime cleanup so retired Meditron drones cannot linger and accumulate across hero switches.
- #70 Restore Kinetic Mine death-drop sector placement and flight timing, correct the explosion radius from 16 to 2.5 units, and retire owned plasma effects with the client-authored burst after the 5-second armed timeout.
- #70 Restore Contagion Pustules' client-authored disease burst to poison nearby enemies within 6 units for 6 seconds, using Life/Energy damage instead of a generic explosion hit.
- #70 Restore Scaldron plants' client-authored destruction bursts instead of generic Zelem explosions, immediately removing small plants and retaining large plants' dead models across campaign maps.
- #70 Restore security and red boss teleporter glows by replacing null-model effect anchors with their authored teleporter models, including campaign reconnect baselines.
- #70 Restore client-authored static blockers marked as dynamic walls, including Infinity's machinery collision boxes, using selected-layout footprints instead of oversized visual bounds.
- #70 Prevent Arcturus's delayed landing from revealing him after death, retire superseded attack callbacks at takeoff, and preserve Twin Laser's client-authored airborne endpoint path instead of snapping it to navigation terrain across all ranks.
- #70 Anchor Dimensionist's Slow Shield bubble and slowing field to the client-authored stationary object instead of the moving caster, restoring cast timing, rank-one visuals and independent field expiry across campaign maps.

### 2026-10-15

- #71 Retire opened obelisks' server collision at the client-authored physics-removal step and preserve that state across Continue and reconnects, preventing lingering blockers in the tutorial and campaign.
- #69 Restore ordinary campaign portals' authored box and sphere trigger geometry, including height, orientation and offsets, fixing narrow portals such as mission 6-4 that failed to transport heroes.
- Add Compact snapshots below the GitHub link in Send Report, moving completed snapshot ZIPs into numbered bundles capped at 25 MB in the background, dimming the button while running and showing a checkmark on success, while retaining originals if packing fails or a source changes.
- #68 Preserve SRS-42's confirmed regular-missile collision target in its explosion damage, fixing direct hits discarded when a destructor or other large enemy's center lies outside the blast radius.
- #67 Allow full, unlocked legacy campaign squads with an unassigned category to enter missions as advertised by the client account response, and identify specific squad validation failures in loading logs.
- Name bug and launcher report ZIPs as message-location-version-timestamp using mission levels or client-reported Arsenal, Navigation and Editor locations, retaining known levels during partial diagnostic failures and using unknown-level when unavailable.

### 2026-10-13

- #66 Remove Menace Weed's healing aura when healing ends or the enemy dies, restoring client-authored effect cleanup across all ranks and captain variants.
- #66 Prevent equipment pickup failures from selecting categories whose compatible catalog items are outside the rolled item-level range, across hero families and campaign maps.
- #66 Prevent Pterodyne's interrupted stealth approaches from retaining overlapping modifiers whose delayed expiry reveals a newer cast, across its ranks and captain variants.
- #66 Restore Necrodactyl's higher-rank attacks from rank-one values to client-authored cooldowns of 2.5/2 seconds, projectile speeds of 15/20 and direct damage of 2/3 across campaign maps.
- #65 Send NPC pursuit and strafing along the server's navigation waypoints and reconcile their physical pose at movement stops, addressing enemies stuck behind obstacles while their server hitboxes advance.
- #65 Restore Caustic Stingers' Homing Swarm attack range from 22 to the client-authored 35 and projectile travel from 28 to 40 across campaign variants.
- #65 Improve Sync Snapshot identity recovery from retained movement samples in the same session and zone, and distinguish removed trigger objects from missing identity mappings.
- #65 Restore Gravity Storm's enemy lift, slam, get-up and interrupted-drop animations, synchronize its channel bar and storm effect position, and preserve damage at landing with control ending after the authored recovery.
- #65 Restore Healing Sprite's sting animation and healing impact with the client-authored three-second cadence, replace snapping with smooth pet following, and synchronize its health and effects with co-op players while retaining the existing heal amount.
- #65 Restore Sleeping Cloud's six-second channel bar, keep its caster stationary throughout the cast, and finish with the authored relax animation and sleep-field cleanup.
- #65 Stop replaying Sporogenesis's activation cloud on every healing tick by emitting one caster-bound effect, preserving the existing healing amounts and timing.
- #65 Restore Tork's Toxic Spores projectiles and poison pools when aiming at empty ground within attack range, and let their poison affect hostile enemies regardless of which co-op hero they target.
- #65 Keep Shielded Grenadiers stationary during shield setup, active stance and teardown; use grenades as their normal attack and reserve shield bash for nearby frontal targets across all ranks and captain variants.
- #65 Fix mission 5-3 Return disconnecting gameplay while preparing the 5-4 preview by excluding unresolved marker-set references from layout selection across campaign maps, while preserving valid empty variants.
- #65 Fix Overdrive activation crashes by sending its ability identity and authored cast timing instead of the hero object ID, and synchronize its active meter through expiration.
- #65 Start campaign missions with empty Overdrive and replace XP-scaled charging with 5 energy per minion and 20 per non-minion, filling the meter after 84 ordinary kills before equipment bonuses.

### 2026-10-11

- #64 Restore Pyro's client-authored fire-breath damage, reach, cone angle, area descriptor and rank cooldowns across all missions instead of the inflated server profile.
- #64 Correct Overdrive capacity from 100 to the packaged 420 so the client can reach charged state, and restore the packaged 70-per-second drain for activation duration and tutorial unlocks.
- #64 Protect heroes from damage during switching and deployment, and move nearby-enemy knockback from arrival start to the client-authored landing time with its authored radius.
- #64 Restore revived enemies to their boss-wave roster so subsequent hits publish their death correctly and the mission waits for them to be defeated; prevent delayed resurrection from reopening a completed arena.
- #64 Restore omitted server-owned tree scenery at its authored placements across campaign maps, including Spectral Forest's yellow trees, while preserving client-owned trees and sharing the restored scenery with co-op and reconnecting players.
- #63 Add a Boss Spawning accordion to launcher Server Settings with saved controls for opening adds per anchor, adds alongside Illust and captain-entry delay; changes apply to new missions and are included when sharing settings to GitHub.
- #63 Include difficulty-eligible, horde-legal special enemies alongside minions in captain-arena waves across campaign maps, restoring candidates such as Hasters, Decelerators and Invincitrons that were excluded from boss adds.
- #63 Resolve hero movement destinations inside static blockers to nearby walkable contact points instead of repeatedly stopping and snapping the hero back, while retaining collision and navigation limits.
- #63 Initialize the native remote-NPC partial movement goal for pursuit, retreat and other movement commands, preventing enemies from remaining at spawn on the client while their server hitboxes advance and then snapping forward when attacking.
- #63 Align Chrono Striker retreat stopping distance with the client Flee script and native footprint calculation, and send fixed-point retreat stopping distances so enemies do not overshoot their server position before attacking.
- #63 Restore Space Barracuda blink landing selection from a preferred rear flank to client-authored random points around the target, checking terrain reachability from the Barracuda and rejecting excessive edge projection.
- #63 Reconcile predicted movement from the incoming hero during deployment instead of silently discarding it, preventing client/server position drift while switching heroes.
- #63 Align NPC object pursuit and redirects with the native target-center goal and separate stopping distance, correcting premature client stops that can lead to drift and visible position corrections for Reparatrons and other enemies.
- #57 Preserve both Exploder Scarabs and Robo-Bombers in Uranium Heights' authored first-run roster instead of replacing Scarabs after the mission preview is selected.
- #57 Change campaign enemy previews from the hard-coded 1-1 list and mixed configuration pools to the selected gameplay minion/special roster, preserving authored first-run enemies and excluding captain equivalents.
- #59 Correct rejected hero movement through the native local-player physics and locomotion receivers, preventing persistent position drift reported alongside tutorial camera-follow failures and keeping co-op peers aligned.
- #59 Initialize Blitz Alpha and Sage Alpha before authentication so completing the tutorial, including with `/victory`, leaves them available in the Arsenal; repair missing starters on existing profiles at login.
- #59 Improve static-obstacle detours on narrow walkways by searching connected navigation-edge points when box-corner routes fail, addressing movement rejection before the tutorial boss teleporter.
- #60 Extend Sync Snapshot packet history from 30 to 60 seconds for snapshots and attached bug-report diagnostics while retaining the 30-second automatic capture interval.
- #60 Restore Helix's first-campaign planet-reinfection narration during Continue navigation by publishing completed progression before the client checks the offered planet's voice cue.
- #60 Fix equipped heroes entering missions with base health and energy by applying the client-authored Strength and Mind resource bonuses for Sage Alpha and all other heroes.
- #60 Prevent delayed NPC actions, target updates, and join snapshots from restarting defeated enemies or recreating their live models after death.
- #60 Give valid movement priority over an active melee swing by cancelling its pending hit and held repeat, and prevent targetless player melee inputs from automatically selecting a nearby enemy.
- #60 Fix enemy attacks gaining extra reach at impact by preserving authored ability ranges instead of applying footprint and stopping-distance adjustments twice.
- #61 Fix attacks sticking immediately after a hero switch by replying to attacks blocked during deployment so the client clears their pending action state.
- #61 Prevent queued damage feedback from restoring a defeated enemy's earlier health, fixing exploding enemies appearing to return after death for players and co-op allies.
- #61 Fix blue and green obelisk hover effects surviving interaction by attaching them to the client object so the authored effect-removal request can stop them.
- #61 Change NPC pursuit redirects from goal-only updates to complete movement commands, preserving client path waypoints and refreshing movement state and stopping distances to address enemies stalling and snapping during combat.

### 2026-10-07

- #58 Add a Wine Runtime option to the launcher Config tab that detects the system Wine, Lutris, Heroic and Bottles Wine builds, Steam Proton and Proton-GE, and starts the game with the selected build in its own launcher-owned prefix.
- #58 Fix Wine Runtime scans skipping Wine and Proton installations linked through directory symlinks.

### 2026-10-06

- #56 Restore Polaris's higher-rank Mark of Zelem cast, thirty-second target mark and reticle cleanup.
- #56 Restore Merak's higher-rank dynamic plasma geysers, accelerating spawn cadence, entry-triggered burning, co-op publication and cleanup.
- #56 Change Orcus's Consume from a nearby-servant opportunistic action to one activation at each authored health threshold, with rank-specific servant rings, following, repeated channel/eat cycles and individual healing; correct normal servant spawning from staggered births to one contact-frame batch.
- #56 Correct Arcturus's minion spawn chance from 20% at every rank to the authored 20%, 25% and 30% progression.
- #55 Fix empty mission 1-3 boss arenas and the same callback-slot mismatch on other boss triggers by resolving the authored boss callback instead of stopping at its event-only entry.
- #55  Fix empty mission 1-3 boss arenas and the same callback-slot mismatch on other boss triggers by resolving the authored boss callback instead of stopping at its event-only entry.
- Fix repeated attacks retaining an old facing by applying captured turns to the controlling client as well as observers, and prevent delayed projectile releases from resetting a newer attack animation.
- Fix blue and green obelisks rejecting interaction by checking reachable contact beside their collision bounds instead of requiring their centers to be walkable.
- Fix post-tutorial heroes appearing locked by refreshing the owned roster, granting Blitz Alpha and Sage Alpha by default, and preserving the third-hero activation credit across duplicate completion calls.
- Correct elite minions from +75% health, +50% damage and +25% size to the client-authored +400%, +150% and +50%, including Mutation Agent promotion and co-op presentation.
- Group Ally Alert controls into a collapsible section with a scrollable settings area in the Server Rules dialog.
- Move Server Rules into a dialog opened from Config and add Share to GitHub with the displayed settings in a TOML code block.
- Move ally-alert tuning from manual configuration edits to live launcher controls backed by server.toml, with section versions that reset outdated overrides to validated defaults.
- Match Reparatron's client-authored repair selection: prioritize eligible friendly Cyber corpses, exclude fading and critical-hit deaths, and share the two-second failed-roll block across co-op ownership changes.
- #53 Add configurable NPC ally-alert propagation for nearby idle enemies, with authored range scaling, bounded relay depth, and optional diagnostics for gameplay comparisons.
- #53 Restore Reparatron's client-authored approach, repeated repair and revival animations, shared co-op repair stacks, corpse-timer refresh and revived enemies' return to combat.
- #53 Fix hero projectile muzzle offsets changing shot direction, preventing close cursor shots from pitching downward and preserving the client-authored spread for Krel and other ranged heroes.
- #53 Fix hero projectile muzzle offsets changing shot direction, preventing close cursor shots from pitching downward and preserving the client-authored spread for Krel and other ranged heroes.
- #53 Restore Magnetic Master variants' client-authored rank-dependent pull ranges and push/pull cooldowns, and use their current movement attributes during pursuit.
- #53 Restore Magnetic Master variants' client-authored rank-dependent pull ranges and push/pull cooldowns, and use their current movement attributes during pursuit.
- #53 Enable automatic Sync Snapshots by default and keep automatic captures silent in chat, while preserving explicit `/ss` command feedback.
- Include a fresh Sync Snapshot with automatic capture history in `/bug` reports when `/ss` auto mode is enabled, recording capture failures without blocking the report.
- Fix Dendrones and other combat pets repeatedly chasing instead of attacking by keeping pursuit destinations inside the hit-range acceptance margin.
- Improve sync reports by sampling companions at capture time and including their active owner-follow goal, speed, and remaining travel time instead of comparing stale committed positions.
- Fix enemy strafing desync by refreshing movement speed from current authored attributes and Swift, haste, and slow effects instead of retaining an older attack speed.
- #52 Fix heroes moving toward enemies while attacking by matching the client's captured-point facing command instead of sending a pursuit target.
- #52 Fix Exploder Scarabs repeatedly restarting instead of exploding when arming and movement processing occur in the same clock tick, preserving their authored 1.54-second fuse.
- #52 Keep Electron Sphere's lightning target budget for living hostile enemies instead of consuming it on allied or unpublished objects.
- #52 Fix ranged attacks briefly refusing to fire near enemies by using the client-authored actor footprints for attack range checks, matching the existing melee path.
- #52 Restore the client-authored blue loot-obelisk and green health-obelisk hovering effects, with shutdown on use and consumed-state cleanup on co-op rejoin.
- #52 Fix captain and elite HUD presentation from unrecognized object IDs to authored NPC affix references, restoring elite name colors and affix descriptions on spawn and rejoin.
- #52 Restore the Horde incoming warning for live miniboss arena waves, including Illust's opening waves, and broadcast it to co-op players without replaying it on rejoin.
- Honor authored captain affix difficulty bands instead of applying every band together, preserving explicit paired affixes such as Illust's Swift and Swift Aura.
- Restore Swift's movement-speed bonus and Swift Aura propagation to nearby friendly enemies, with non-stacking overlap, departure/death cleanup, and synchronized co-op/rejoin state.
- Fix the 1-1 boss failing to appear by resolving its trigger-owned spawn anchor and selecting its named captain from the boss roster instead of the lieutenant pool.
- #51 Restore Voltroid's discharge beam on the shared attack-impact path, using the authored visual strength for its charge count.
- #51 Restore Terminal Haven's scenery fire vents and plasma-pond damage through shared hazard handling across selected campaign layouts.
- #51 Fix Gravity Well leaving pulled enemies floating by resetting their reaction at landing before the authored recovery ends.
- #51 Restore Shielded Grenadier's frontal shield-bash damage and animated knockback, and allow field and periodic damage through its directional shield while retaining special full-immunity shields.
- #51 Restore authored static collision blockers around Infinity mining machines so movement routes around them instead of passing through them.
- #51 Fix stuck Phantom Charge and Shade Drifter movement by preserving client navigation agents, and retire charge poses after completion or interruption without resetting newer actions.

### 2026-10-05

- Remove reflected request text from HTTP URI errors and prevent untyped traced responses from being interpreted as HTML.
- Restore factory fire vents with repeated warning, eruption and hero damage cycles, synchronized across co-op players.
- Change Laser Tank from cast-limited beams to persistent simultaneous laser zones with client-authored rank limits and cleanup.
- Restore factory pipes' enemy-damaging explosions and persistent wrecks, and fuel canisters' authored blast and burning chain reactions.
- Restore Exploder Scarab's timed accelerated chase and explosion, and apply a three-second daze that blocks actions while allowing slowed walking in solo and co-op.
- Restore selected Citadel plasma ponds' authored burning status, effect and damage while heroes stand in them, including stationary contact and co-op presentation.
- Change Phantom Charge from walking around enemies to the client-authored flat slide through them, restoring collision when the charge ends or is interrupted.
- Restore Gravity Well's pull to scaled footprint contact and Kinetic Wave's animated knockback, preventing enemy pursuit from interrupting their displacement.
- Restore Essence Volley's firing and ending animations across heroes, and extend untargeted projectiles from cursor distance to the authored 25-unit travel budget.
- Correct Grappling Pulsar's Puller follow-up from projectile travel plus pull duration to its authored 1.25-second release, and require footprint contact for each Fast Swipe hit.
- Correct Protoplasm's attack pauses from the full OozeGrow cooldown to its animation release, keeping growth on an independent cooldown, and restore the authored OozePassive green drip effect.

### 2026-10-04

- Remove logged-out profiles from lobby rosters, preventing a crash when a deleted Crogenitor is recreated with the same name.
- Fix Uranium Heights (4-1) failing before heroes spawn by resolving Robo-Bomber replacements from the full noun catalog instead of requiring them in the selected agent pool.
- Keep tutorial arena progression in its wave controller instead of repeatedly attempting to spawn an unavailable campaign boss.
- Align the tutorial horde's western spawn point with the position accepted by the client, preventing enemies from starting several units away from their server position.
- Sample the local hero's retained movement at capture time in Sync Snapshots, avoiding false position discrepancies from stale command-time coordinates.
- Fix tutorial obelisks and other equipment drops failing because temporary Blitz and Sage lacked their loot-selection identity.
- Prevent stale attack cursor positions from relocating idle enemies and causing client/server desynchronization.
- Prevent duplicate Health full and Power full notices when movement commands recheck capsule contacts already processed by movement polling.
- Fix Infectors, Quadra, Shocker and other projectile enemies becoming idle after a stun or chase, and restore the related energy-buff chase continuation.
- Stop and resume enemy pursuit on clients when stun, sleep, root or zero movement speed pauses it on the server, preventing enemies from walking away during crowd control in solo and co-op.
- Honor authored invisible enemy starts and alternate visible idles across campaign variants, restore entrance animations, and retain reveal state across co-op rejoin and Continue.
- Keep tutorial enemies hidden until their teleport entrance reveals them, preserving pending reveals across rejoin and interrupted entrances.
- Correct Seraph-XS and other hero variant profiles showing Jinx's name and description by resolving localization keys within each hero's authored text table.
- Restore Toxicactus's authored destruction event and 0.2-second removal timing instead of the generic scenery explosion.
- Correct HELIX return-to-planet narration from one-mission-late assignments to the authored campaign stages, and select the next mission's narration on the post-mission screen.
- Change Destructor equipment drops from six fixed points to client-derived party-scaled attempts and shuffled expanding rings with navigation checks, retaining authored catalyst eligibility.
- Advance unanswered post-mission Continue/Rewards countdowns to rewards for the party when time expires, preserving the remaining time across repeated voting-screen requests.
- Restore Plasmatic Seed's plasma explosion and Necrotic Plant's bursting effect from their authored death scripts, replacing the machinery explosion and missing burst with correctly positioned effects and deletion timing.
- Correct Cryos geysers from disappearing ice debris to persistent opened vents that erupt after being broken, with owner-bound warning audio, authored burst orientation and restored vents on reconnect.
- Restore loot and health obelisks across campaign maps from authored component placements, preserving seeded variants, use limits and noun-inherited reward budgets without requiring nonexistent event-script bindings.
- Change campaign horde and boss activation from immediate radius checks to authored trigger shapes, party-entry requirements and wait times, preserving unfinished persistent waits across checkpoints and preventing early boss fallback activation.
- Stop spurious "Objective 00:02" arrival notifications by removing the forced HELIX introduction that incorrectly announced the mission timer.
- Restore Herbipod detection from the shortened 5/7/9-unit aggro ranges to their authored 19/20/21-unit perception ranges, consistently across visibility, acquisition, and retargeting.
- Correct campaign enemy composition from shifted science fields and fixed map rosters to the client's authored auxiliary science fields and per-section enemy choices.
- Prevent delayed miniboss admissions from replacing an active or completed encounter and spawning its enemies again.
- Restore miniboss opening enemies at authored arena spawn points before the captain appears, including Catalyst and Overdrive tutorial arenas.
- Restore the correct miniboss phase when reconnecting during the wave transition or after the captain appears.
- Change ordinary lieutenant population from captain variants to the authored special-enemy roster, preserving separate captain and boss encounters.
- Stop NPC movement at the last accepted position when pathfinding fails instead of advancing through blocked terrain and accepting false attack arrivals.

### 2026-10-03

- Restore Nashira's scripted entrance facing, correct Polaris's teleport center and orientation, and prevent interrupted boss callbacks from applying stale movement or activation.
- Complete distant pickup commands on arrival without requiring a second click, canceling retained pickup pursuit when movement changes.
- Limit each capsule drop batch to one resurrection capsule so consuming it does not expose an overlapping spare.
- Keep /kill processing after an owner's defeat also defeats its owned NPCs, skipping those stale batch targets instead of aborting publication.
- Use authored NPC loot budgets and client-backed co-op equipment/catalyst attempt counts, replacing fixed budgets, ordinary drop-chance pity, and guaranteed ordinary boss equipment.
- Choose health and power capsules using the whole co-op party's squad resources, with a separate selection RNG preserved across checkpoint restores.
- Remove inflated Gravitic DNA and capsule overrides, and match client rounding when calculating capsule drop budgets.
- Use authored DNA drop chance and minimum stage, reducing ordinary drops from 50% to the installed 25%.
- Reduce health obelisks from four or five capsule batches to one authored batch, and use each enemy's authored challenge for capsule drop budgets.
- Collect landed capsules and DNA while standing still, retain pickups crossed in large groups, and prevent premature collection after teleporting.
- Keep aura status effects synchronized across co-op clients when a scan fails or the caster leaves, and queue removals before reusing modifier handles.
- Preserve capsule and DNA pickup messages when a later operation fails, including full-resource notices and accepted DNA balance updates.
- Prevent persistent aura failures from refunding already activated casts or overwriting later power changes; refund failed startup only once.
- Make Time Bubble slow shared co-op projectiles and preserve the slow until the last overlapping bubble releases them, without overwriting other speed changes.
- Prevent late drain and Gravity Storm failures from refunding activated casts or overwriting newer power changes; refund failed unpublished admission only once.
- Apply projectile freeze and destruction across co-op players, including Arcturus missiles, with ordered projectile and effect cleanup.
- Let shared freezes expire after the caster disconnects or switches heroes while preserving later overlapping deadlines.
- Remove interrupted drain and Gravity Storm effects for co-op allies and prevent canceled callbacks from recreating them or applying damage.
- Preserve existing projectile freezes and movement when another freeze fails to initialize, and retain updates for earlier successful freezes.
- Preserve charge-applied statuses across later actions, expire each at its own deadline, and remove statuses and attached effects reliably for co-op allies on departure.
- Prevent an expired charge effect from resetting a newer hero animation, and keep charge-release command responses private to the caster.
- Expire transferred buffs and temporary maximum-health bonuses after caster disconnects, and show their removal to co-op allies.
- Keep an admitted pet charge running after the hero's impact when the hero starts another action, while canceling stale pet movement safely.
- Remove failed hero and enemy projectile flights from co-op clients, with ordered cleanup of remaining freeze effects.
- Detect teleporter crossings between movement polls while preventing false crossings after teleports or hero changes.
- Stop enemies' old attacks and lingering laser beams when pulled, and leave their pose and attack unchanged if pull setup fails.
- Restore companions' ongoing follow movement on reconnect and publish their final position to co-op peers.
- Restrict allied healing and transferred buffs to active participants and their current companions, excluding players in results, reconnect or disconnected states.
- Prevent stale companion pursuit callbacks from moving pets or canceling newer pursuits after teleport, replacement or death.
- Restore companion maximum health and active enrage size changes on reconnect without replaying healing or restarting effects.
- Restore remaining active and squad-support ability cooldowns after reconnect without restarting their timers.
- Restrict Sage's Dendrone attacks and pursuits to that player's current summons so allies' pets keep their own targets and cooldowns.
- Make qualifying allied hits wake Sleeping Cloud targets and remove the matching sleep effects for all players in the zone.
- Retain Field Medic's first transferred companion buffs in session tracking so their expiry can remove the buffs and release their modifier instances.
- Refresh surviving squad portraits and clear client deploy cooldowns after a hero dies, and handle death selection as an immediate replacement instead of a voluntary swap.
- Stop active security and boss teleporter effects when enemies block the pad, prevent duplicate portal loops, and refresh their displayed state after reconnect.
- Make enemy cleanses remove physical vulnerability and its extra physical damage taken.
- Calculate shared health and power capsule bonuses per receiving player, and exclude disconnected, reconnecting or finished players from capsule and DNA sharing.
- Cancel Fire Tempest pet shots still winding up when the pet teleports, then resume targeting from its new position after the existing cooldown.
- Change security teleporter enemy detection from a 4.8-unit estimate to the authored 20-unit radius, excluding allies and enemies explicitly hidden from security checks.
- Restore 1-2 boss progression after the red portal by honoring the authored four-second arena trigger and publishing the encounter to all players.
- Show still-active allied Trees of Life after reconnect using their retained objects and original simulation expiry, without replaying growth, costs, cooldowns or healing.
- Audit selected campaign layouts with per-section population counts and fixture identity, transform, and load/rejoin takeover logs.
- Remove Tree of Life reliably for watching allies when its caster beams out or disconnects, clear retired tree state before rejoin, and prevent late cleanup from affecting a replacement tree.
- Restore mission 1-2 enemy spawn candidates by reading authored marker sections before falling back to marker-set filenames.
- Fix the Gravitic Stabilizer debuff visual lingering after leaving its shield by using the client's one-based effect slot for attachment and removal.
- Fix Return to Ship retries after an interrupted victory by reusing the saved XP receipt and preparing the next-mission preview before committing XP.
- Fix Return to Ship disconnecting during the next-mission preview when horde markers lack an agent pool; use the mission's minion roster for horde and boss adds.

### 2026-10-02
- Change campaign melee impacts from a fresh distance veto to authored start-range movement grace, retained multi-hit arcs and hostile fallback selection; use native footprint overlap at arc edges and publish the selected victim through the normal damage path.
- Change hero, NPC and companion melee admission, pursuit and hit-arc radii from legacy physics fallbacks to authored noun footprints and live object scale, completing noun mappings and preserving separate projectile geometry without requiring navigation meshes.
- Fix content.db import failures by passing the build context into combat, weapon, and spike resource readers.
- Track content.db import stage durations, index resource identities before projections, reuse bounded package readers, and remove unused duplicate raw resource blobs while retaining Lua and navigation payloads.
- Fix campaign startup going black when a seeded zelems_1 layout has no obelisks by replacing the legacy fixed-count requirement with the authored selection.
- Use each Orcus servant's complete authored, difficulty-adjusted NPC profile in place of the hard-coded stat scaffold, including defenses, AI graph and drop metadata.
- Apply Merak's authored rank thresholds of 15%, 10% and 5%, retain accrued damage at equality, and spend earned add budgets before capacity limiting.
- Fix content.db verification from requiring all catalyst noun definitions to equal the 192 drop candidates to checking candidate coverage while retaining additional authored definitions; advance the import recipe to 88.
- Recover percent-named AI conditions from exact authored graph references during content.db import, preserving spaces and punctuation and rebuilding older imports with recipe 87.
- Change catalyst pickup, movement and removal from contiguous slot limits to the native grid unlock order, preserving tutorial gates and saved and replicated slot identities.
- Share Nashira's 12-fiend budget across the original and duplicates through explicit passive membership, removing the separate fiend cooldown and preserving registration, rollback and owner teardown across checkpoints.
- Resolve Nashira fiend stats, AI and loot metadata from the full rank-selected child noun profile, preserving run scaling and diagnosing missing definitions.
- Resolve teleport endpoints from all seeded selected marker definitions instead of filtered director placements, preserving full IDs, later-selected duplicate overwrites and missing-destination behavior.
- Retain authored catalyst noun modifier identity and line-bonus tuning in content.db, exposing both to pickup and effect consumers without changing current effect calculations.
- Route campaign named events by full simulator event hash across selected live listener owners, retaining duplicate subscriptions, empty dispatches and native swap-removal on owner destruction.

- Separate scaled actor navigation footprints from unscaled noun spawn radii, retaining actor navigation layers across movement and restore while preserving combat footprints.
- Retain authored NPC AI graphs and gambit overrides, initialize the first start phase without RNG, and preserve node/gambit cursors through version-6 checkpoints without inferring phase execution behavior.
- Select navigation layers from imported NavPower tuning radii with the native backward scan, including oversized actors and the Arena radius cap.
- Use party completion and imported campaign bounds for support, catalyst and overdrive tutorial activation, rechecking unbeaten players at mutation time and resolving the triggering player�s current hero after the authored waits.
- Expire shared catalyst and orb pickups from their original zone deadline, stop cleanup at zone completion, and prevent reconnects, full-inventory hops or delayed collection from extending pickup life.
- Use the shared simulator RNG for native Lua random bounds and float32 draws, preserving equal-bound draws and correcting Corruptor portals from one-or-two attempts to one.
- Filter generated equipment catalogs by native slot/science intersections, inclusive item levels and rarity flags, selecting affixes from the chosen base item science mask instead of hero class/science.
- Land ordinary loot from elapsed lob duration at the exact destination with zero movement, retaining flight state for shared pickup validation and rejoin while preserving projectile collision locomotion.
- Require the imported equipment-enable setting and equipment category mask before NPC or object equipment RNG draws, preserving independent drop categories and explicit developer guarantees.
- Reject unsupported ordinary equipment rarities before world registration or presentation and on rejoin, preserving inventory and unique reward ground drops through explicit server policies.
- Preserve native ground-only flight flags for orb, DNA and equipment drops while keeping ordinary catalyst flight flags false.
- Calculate equipment levels from authored LootPreferences and explicit boss source context instead of granting every fourth mission a boss bonus, preserving unique-rarity and Cash Out chain adjustments.
- Let same-position loot drops hop vertically with the recovered lob fallback, and serialize adjusted height for upward destinations.
- Preserve next-stage reward previews when a campaign chain ends, using the separate continuation limit to force Cash Out at terminal stages and the five-planet server cap.
- Reproduce catalyst level and noun selection with configured campaign minor counts, authored-order float32 draws and cumulative weights, preserving empty-pool draws and no-pickup tails with explicit server pity policy.
- #42 Resolve spawned NPC affix modifiers from imported asset names and stored IDs instead of synthesized names, correcting Spiky and persistent-aura bindings while preserving explicit parent/child co-occurrence.
- Separate NPC perception queries from acquisition, using authored aggro/alert ranges and an owner-centered pet offset derived from level yaw and retained across restore.
- Show catalog asset and source identities for levels and selected marker sets in map diagnostics while keeping authored layout reference order.
- Separate ordinary catalyst drops from tutorial attempts, rejecting stages below the minimum before the chance draw and selecting once without a stage clamp or extra guaranteed roll.
- #42 Change campaign resurrection capsules from picker-only full-squad restoration to each eligible party member's first fallen slot, using authored recovery fractions and recipient orb effectiveness with party rollback and ally resource publication.
- Fix catalyst pity from a maximum 1% chance to percentage-point boosts and a guaranteed chance roll after 55 misses, preserving native truncation and explicit zero overrides.
- Separate NPC loot and XP suppression, retain loot-only resurrection and Doppler clone flags, preserve both Nashira duplicate flags, and allow ordinary summons to reward independently of encounter cleanup.
- Preserve packaged catalog order when selecting campaign equipment bases, prefixes, and suffixes, appending entries without catalog ordinals after authored entries.
- Gate campaign equipment drop categories by authored mission-stage minimums instead of account level, unlocking feet at stage 9 and graspers at stage 17 while preserving item-level checks.
- Select a second prefix for epic campaign equipment with repeated distinct-index draws, retaining duplicate prefixes when the eligible pool contains one entry.
- #42 Resolve interactable zero challenge budgets from authored noun definitions instead of ability-specific 500/100 defaults, preserving whole marker overrides and nonzero budgets.
- Carry authored noun-category IDs into spawn metadata and validate loot creation and rejoin projections against native categories, keeping DNA and equipment payloads explicit.
- Reject incomplete generated campaign equipment before world pickup or inventory publication, preserving the selected rarity and base item when catalog definitions are missing.
- Unlock campaign weapon implicit stats from signed item-derived campaign stages and the first authored weapon owner, preserving Arena weapon calculations.
- #42 Move equipment winner selection, materialization, and collection from pre-wait selection and 400 ms grants to the 100 ms commit boundary, starting the picker animation immediately and retaining release at 400 ms.
- Calculate item stat bonuses from positive combined affix attributes instead of including standard slot stats, preserving signed cancellation and float32 addition order.
- Preserve native item-price float32 boundaries, signed truncation, and integer increment rounding when calculating new prices, retaining invalid-value fallback and overflow saturation.
- #42 Import complete marker and noun interactable definitions, preserving ability and emitted-event slots while removing false listener subscriptions.
- #42 Preserve authored combatant death-event bindings separately from noun combatant admission, including non-combatant scenery definitions.
- #42 Import marker spawn triggers and ordered event listeners from reflected records, preserving geometry, timing, flags, callback slots, hashes and overrides instead of guessing from strings.
- #42 Preserve noun pickup triggers and event listeners in content.db, and expose authored resurrection health fractions with a separate missing-property fallback.
- #42 Import NPC affix definitions with modifier mappings and parent/child links, and preserve captain-affix difficulty bounds through structural class decoding.
- #42 Allow server startup with authored affixes whose eligibility lists are empty, preserving their identity and stats while requiring nonempty category matches during selection.
- #42 Validate asset-catalog links from authored entries to packaged resources, allowing uncataloged package assets during content preparation.
- #42 Preserve finite authored noun bounds with reversed endpoints in content.db instead of rejecting shipped assets during content preparation.
- #42 Fix content preparation when a marker's noun-shaped name differs from its noun reference, keeping adjacent component bindings out of the marker count.
- #42 Fix content preparation for marker sets with adjacent component bindings and repeated noun-name references, preventing spurious extra markers.
- #42 Fix content preparation for affixes with modifier-grant or ability-improvement arrays by decoding their complete reflected tails.
- #42 Import the campaign minor-stage count and recipient sidekicking settings into content.db, exposing validated reward tuning with separate authored values and native defaults.
- #42 Fix content preparation for teleporters with custom marker names by reading both authored references before their component data.
- #42 Import ordered asset identities, DNA reward properties, affix slot eligibility, and every noun's lifetime and projectile-definition presence into content.db with source provenance.
- Prefix errors copied from the launcher with the Darkspinner version.
- #42 Preserve authored movement tuning, door interaction inputs, generic teleporter definitions and the packaged equipment-drop gate in content.db, retaining absent definitions and settings.
- #42 Allow launcher content preparation to retain unresolved noun AI references instead of rejecting shipped nouns whose AI definition is absent.
- #42 Expose navigation-obstacle inputs for every noun in content.db, including unnamed resources and nouns without NPC classes, while preserving authored enums and bounds.
- #42 Fix launcher content preparation by decoding level roster headers from authored presence fields instead of ambiguous header scanning.
- #42 Preserve absent level cameras instead of decoding filename text, and expose authored AI graphs, idle/death hooks, cooldown operands, elite settings, and reward tuning through content.db readers.
- #42 Scale orb drop budgets by the authored stage tuning and roll every 100-point attempt, including guaranteed drops, retaining and expiring all emitted pickups.
- #42 Import authored NPC aggro modes and gate explicit alerts and added threat, preserving duplicate alerts, first-alert state, and reciprocal attacker tracking.
- #42 Align NPC idle and combat movement with authored speeds and movement buffs, and replicate combat state consistently with target acquisition and Arena mode.
- #42 Change Corruptor portals from boss-wide batch filling to individual timed spawn attempts with per-portal live counts and authored roster/cap overrides.
- #42 Gate enemy orb, catalyst, equipment, and DNA rewards by authored drop-category masks before their individual reward rolls.
- #42 Replace portal summons from a fixed ring to eight scatter attempts using a separate native LCG, authored size-class bounds, navigation and collision checks, and origin fallback.
- #42 Select Corruptor portal enemies from each phase's authored LevelConfig and the TNX-173 default using stage-eligible uniform random draws instead of inferred rank rosters and cyclic selection, including entries excluded from hordes.
- #42 Move mission 1-1 support activation from a 13-second boss admission estimate to the scripted 15-second first-clear or 2-second replay call, retaining the 6-second first-clear unlock.
- #42 Compose horde groups from authored minion and special rosters with challenge budgets, stage-dependent special ceilings, section-pool expansion, and a 15-member limit.
- #42 Compose budgeted spike groups from section rosters and authored challenge costs, replacing forced captains with mixed minion, special, and conditional agent selection.
- #42 Extend native seeded layout selection from mission 1-1 to every loaded map, sharing selected marker sets across NPCs, destructibles, obelisks, and teleporters while preserving empty alternatives.
- #42 Remove ordinary spike markers that overlap authored horde-trigger or boss exclusion volumes before campaign group planning.
- #42 Import the director's consumed ServerData composition properties and load group-cost scaling from the packaged 0.2 override instead of executable fallback or fixed server constants.
- #42 Select first-time campaign enemy rosters from every party member's progress instead of only the requesting player's, falling back to normal composition when no first-time minion qualifies.
- #42 Share minion and special roster bags across campaign sections A/B/C, refilling each only after its eligible archetypes have been drawn.
- Resolve NPC profiles from authored noun and class references instead of shared stat filenames, preserving captain identities and ZelemSpecialThree_3's rank-3 awareness ranges.
- #42 Import external planet and science enemy rosters and compose campaign enemies from local-only selection to authored mixed sources, including the stage-25 and stage-49 transitions.
- #42 Replace spike group sizing from a fixed 2–6 estimate to challenge-budget composition with repeat-eligible noun draws, float32 group costs, and a 15-member cap.
- #42 Import authored normal and first-time enemy rosters for every map with local configuration data, replacing string-scanned pools while preserving stage limits and horde eligibility.
- Select campaign section buckets from the stage's chapter instead of always using chapter 1, while retaining full-stage enemy roster limits.
- #42 Persist HELIX's pending mission introduction in the active session so it can play after hero arrival.
- #42 Remove Haster buffs from defeated enemies and prevent delayed buff expiry from deleting a reused modifier.
- #42 Stop promoting first-clear 1-1 special-roster enemies to captains with elite stats and affixes; retain elite treatment for authored captain entries.
- #42 Fix mission 1-1's black loading screen by retaining every authored marker set during map-layout selection, including obelisks, script-only sets, and empty alternatives.

### 2026-10-01

- #42 Align 1-1 destructibles, obelisks, and ordinary enemy placement variants with the client's map seed, replacing independent layout rolls and the forced first-clear health-obelisk position.
- #42 Select 1-1's authored horde alternative, including the empty variant, remove boss prerequisites on unselected hordes, and preserve horde and boss-add choices from the run seed across restoration.
- #42 Correct mission prepare/start packets from player slots and party masks to campaign stages and map conditions, with a shared map seed preserved through rejoin and checkpoint restore.
- #42 Preserve authored creature type, drop-type choices, combat speed, planet-config references, director difficulty tuning, and marker-set conditions in campaign content; use authored combat speed for NPC movement.
- #42 Import authored NPC type and noncombat movement speed for campaign enemies, using the speed for mobile idle movement while keeping preplaced dormant enemies stationary.
- #42 Replace the fixed 12-unit campaign enemy aggro boundary across all levels with each noun's authored NonPlayerClass range, including bosses and scripted adds, retaining 12 only for missing ranges.
- #42 Import authored SectionConfig buckets and use their difficulty-1 archetype counts to give first-run 1-1 sections distinct enemy rosters while retaining the provisional entrance placement.
- #43 Make first-run opening enemy selection repeatable for every chain mission, with per-level seeds and spawn-point logging for placement checks.
- #43  Remove Vex's Time Bubble visual and affected-target modifiers when an aura is stopped by a reset or scheduling failure, using the same client cleanup as normal expiry.
- #43 Restore Vex's Time Lapse from a single-target melee hit to a nearby area attack that repeats each enemy's last 10 seconds of damage up to its Dexterity-scaled cap, with the authored hit effects.
- #43 Recheck melee reach at impact for Protonic Sword and other melee basics, preventing damage to targets that have moved out of reach and using the hero's current position for cleave selection.
- #43 Restore Rocket Barrage's cast-duration bar and animation cleanup, allow cancellation throughout the cast, and stop further launches without discarding rockets already in flight.
- #43 Restore SRS-42's rocket proximity homing without a stationary prerequisite and apply the authored 3-meter explosion radius, including area-radius bonuses, instead of single-target damage.
- #43 Keep Rooted heroes at their authoritative position, stop existing pursuit, and block attack pursuit, distant trap placement, and following until the root expires while preserving attacks already in range.
- #43 Send Chrono Blink's Frozen state to enemies and projectiles so their client animations and flight pause until the freeze expires, preserving overlapping freeze durations.
- #43 Move SRS-42's Targeting Computer radar from invalid attachment slot 31 to an allocated client-supported slot, allowing movement and hero switching to remove it without stacking scans.
- #43 Preserve ranged attack animation recovery when held fire is released, preventing a following movement command from clearing the current attack's release timer.
- #43 Remove Death's Embrace's duplicate, untracked Terrified visual so its status modifier alone controls cleanup on expiry and enemy death.
- #43 Enable the client notification flag required to play HELIX's blue-obelisk access and mission-introduction voice cues; keep health obelisks and silent objective updates excluded.
-#42 Defer HELIX's mission introduction until dungeon setup commits and hero arrival finishes, instead of sending it in the first gameplay frame; log delivery for playback diagnosis.
- Run launcher self-updates first through the Patch button and Auto Patch; startup only checks availability, and an update restarts the launcher before game patching begins.
- #41 Restore authored targetable destructibles across missions 1-1 through 6-4 with matching scenery layouts and shared destruction state.
- #42 Move mission 1-1's first-clear population from synthetic formations to authored spawn candidates and roster pools, including horde and boss adds; preserve marker sections and weighted alternatives, restore its first standing repair mob and nearby elite group, and replace the speculative on-screen Haster beam-in with standing section population.
- #37 Update content verification from 2,288 to all 2,408 shipped loot definitions, including the 120 restored weapons.
- #37 Allow shipped loot definitions with empty level ranges such as 999..100 through content parsing and database storage, rebuild older content schemas, and exclude these items from generated drops and nearest-level fallback.
- #40 Expand hordes from Outer Rings onward to three independently spawned waves with a final Mutation Agent that promotes nearby enemies to elites; close authored arena gates, publish horde state through completion and reconnect, and move HELIX's Zelem reinfection cue to mission 2-3.
- #38 Give Dimensionist shields the Gravitic Stabilizer's hero status and projectile slowing behavior for every nearby player, using each shield's own radius and lifetime.
- #38 Make Frigid Caverns' Toxic Fungus attackable with its authored 5 HP and death graphics, poison-stalk explosion, and a short-lived cloud that poisons heroes on contact.
- #38 Correct Cryos geyser particle packets, repeat bursts every ten seconds, and apply damage and lingering burns even when heroes stand still.
- #38 Allow Overdrive to recharge whenever its meter is below full, double kill energy gains, and award at least four energy per kill before equipment bonuses.
- #38 Honor squad changes when continuing between missions, retain each player's selection through party voting, and initialize the new squad's own health and energy limits.
- #38 Balance Chaos Fields' planned small-enemy population across Strafing Drakon, Chrono Striker, and Blasting Fiend in equal shares while preserving spawn locations and total population.
- #38 Balance Outer Rings' planned small-enemy population across Pincering Carapace, Sting Raider, and Robo-Bomber in equal shares while preserving spawn locations and total population.
- #38 Restrict Gravitic Regulator and Gravitic Stabilizer loot to a 75% DNA chance and a separate 10% health-or-energy chance, and allow catalyst drops from totems only among destructibles.
- #38 Make Pincering Carapace use its basic bite before charging its knockback and between charge cycles, preventing long pursuits from skipping its simpler attack.
- #37 Restore 120 missing weapon definitions across Zrin, Goliath, and ten other hero families, fixing missing drops and incorrect Utility Slot metadata, and restrict hero weapon drops to their authored family instead of cross-family substitutes.
- #38 Finish enemy knockback reactions on heroes with the native landing/outro transition instead of leaving the airborne animation looping, including for multiplayer allies.
- #38 Restore Gravitic Stabilizer shield status presentation on heroes and clear it when they leave the field or the stabilizer is destroyed.
- #38 Expand Gravitic Stabilizer combat bounds from the small fallback footprint to its authored body and corners, and retain its destroyed model and collision after death and reconnect.
- #38 Slow hero projectiles inside Gravitic Stabilizer shields, including bursts, thrown shots, cloud lobs, and companion shots, while delaying impact damage and starting ground effects on arrival.

### 2026-09-30

- Let melee swings finish as misses when their target dies before impact, preserving the attack release instead of aborting the animation schedule.
- Fix enemy projectiles subtracting damage twice from client world health, preventing false hero-death cues while the server and squad HUD still show surviving health.
- Apply Decelerator's attack-speed debuff to authoritative hero attack timing and restore normal timing when the modifier is removed.
- Fix Titan's Proximity Mine walking into range without placing the mine, retain the selected ground destination, and stop movement before playing its authored placement animation.
- Make Toxicactus destructible across Verdanth by replacing scenery-only instances with damageable objects using its authored health and targetability.
- Keep PvP available from level 1 with the first Arena squad automatically unlocked.
- Retire health-drain channels before restarting enemy attacks and prevent stale channel callbacks from interrupting or restarting a newer attack.
- Restore clickable resurrection capsules with their native pickup animation and timing, including after reconnecting, and share capsule removal and revived squad health with allies.
- Fix Shade Drifter charges stopping at the hero by sending their authored destination beyond the target and preserving the charge animation and travel direction.
- Restore Botanical Tunneler's underground digging animation and moving earth effect, clearing the effect on emergence or cancellation without changing its attack timing or damage.
- Restore large and small Ancient Totems across Verdanth at their authored placements and sizes, play their destruction effect and sound, and restrict their loot to catalysts with a 75% base drop chance.
- Fix mission 3-2 Frigid Caverns hanging on a black loading screen by loading its three authored cave scenery sets before mission initialization.
- Change Ray Killer's single-target lightning shot to a seven-bolt forward fan with muzzle effects, and fix retreating from damage or nearby heroes before resuming ranged attacks, including captain variants.
- Keep Reconstruct's channel bar visible for its default three-second healing window, independent of cooldown reduction, and stop healing and effects when the button is released or movement interrupts it.
- Restore authored Gravitic Regulators and destructible Gravitic Stabilizers in Outer Ring and Chaos Fields, including stabilizer shields that slow heroes by 40% within 12.5 units and clear on exit or destruction.
- Increase Destructor rewards from one equipment item to six arranged around the fallen boss, with 60% rare and 15% epic odds per item.
- Resume idle Destructor combat automatically while a living target remains and restart Arcturus correctly after an interrupted Twin Laser.
- Hold Destructors in place during their entrance cinematics, restore Arcturus and Corruptor entrance animations, and keep entrance timing independent of the selected attack.
- Make Arachno Striker's homing projectiles track moving heroes when resolving hits so movement alone no longer prevents terror.
- Make terrified heroes automatically panic-walk around where fear began and stop when fear expires or is removed.

### 2026-09-29

- Make Swarming Herbipods engage from a shorter range and let Homing Swarm projectiles miss moving heroes and expire promptly.
- Attach SRS-42's Targeting Computer effect to the hero and remove it immediately when movement or hero switching begins.
- Require an explicit pickup interaction to consume resurrection capsules, leaving them available when no squad hero is defeated.
- Restore the hero portrait cooldown wipe after switching and keep held right-click attacks aimed at the latest cursor target without interrupting the active strike.
- Stop ordinary enemy pursuit locomotion at its collision-safe attack goal so enemies do not physically shove heroes while approaching.
- Simplify launcher bug reports to a title-only form used for the diagnostic ZIP name, with full issue details entered on GitHub.
- Let Space Barracudas teleport again after each ranged shot instead of remaining at their first blink destination.
- Remove Zrin's Pain Hounds at the end of their lifetime for every player in the mission.
- Recover hero saves containing stale client inventory rows while retaining only equipped parts the account owns.
- Restore Nightmare Vine root damage and Nocturna supernatural plants' authored spectral death presentation, and prevent their terror effect from crashing scheduled combat.
- Correct the completed and next mission names shown by the campaign reward vote.
- Restore Verdanth's destructible ancient stone totems with their destroyed model and a high catalyst drop chance.
- Keep Rezzers engaged by pursuing into Ghostly Bolt range when no resurrection target is available.
- Allow Nocturna missions with fixture-only smart-object sets to finish loading instead of disconnecting at a black screen when their scenery composition contains only deletions.
- Keep each in-mission equipment reward visually distinct in Recently Acquired while reserving its permanent inventory identity until mission completion.
- Restore Invincitron's hover-drone flight animation by driving its orbit through moving locomotion goals instead of repeated stop-position snaps.
- Select mission equipment for the deployed squad and rotate weapon rewards across its heroes before repeating one, preventing an unlocked hero's weapon family from dominating drops.

### 2026-09-28

- Restore Nocturna's destructive plants and Nightmare Vines as attackable fixtures across missions 1-3 and 1-4, including vine collapse effects, damaging live roots, dead-root transitions, and nearby enemy terror on destruction.
- Start Nashira's boss state and music with her reveal and camera focus instead of waiting for her entrance sequence to finish.
- Keep hero-switch landing knockback working in tight terrain by falling back to shorter reachable push distances.
- Make Vampiric Leapers hop toward nearby heroes and deal their landing damage instead of repeatedly jumping in place without hitting.
- Restore missing tree scenery in Nocturna mission 5-2 by composing all three authored smart-object sets.
- Stop Sync Snapshot from treating the Field Medic drone's normal client-authored orbit as a client/server position divergence.
- Restore in-mission equipment dropping by resolving temporary client identities against collected mission loot and returning the item to the ground.
- Show the generated equipment's correct visual when it is picked up by using the native recipient field and keeping transient ground loot out of the client's persistent item identity cache.
- Restore authored Nightmare Vine fixtures and their matching destructible roots throughout Nocturna, including mission 5-2.
- Restore the missing exterior roots, trees, and other authored smart-object scenery in Nocturna mission 5-1.
- Allow mission 1-4 to finish loading by including all authored Nocturna obelisk and smart-object scenery sets in campaign setup.
- Restore SRS-42's Targeting Computer scanner presentation and scanning sound when becoming stationary, and let its damage bonus build each second up to five stacks at 5% normally or 12% during Overdrive until movement resumes.
- Change Thornado's green tornado from one effect on every struck enemy to one caster-centered effect that also plays when no enemies are nearby.
- Restore HELIX's authored map introduction and portrait for every squad on campaign mission entry, plus the authored return-to-planet narration at the corresponding first-pass campaign transitions.
- Keep the mission-entry landing animation synchronized with its sound and ground shake even when zone initialization or setup preparation takes longer than usual.
- Play HELIX's authored obelisk-access line immediately after each successful blue loot-obelisk activation while keeping green health and energy obelisks silent.
- Keep catalysts locked while entering mission 1-3, then enable their drops, pickups, inventory controls, and bonuses when the scripted catalyst unlock occurs.
- Keep weapons in campaign loot rotation for heroes without a dedicated packaged weapon family by selecting the closest authored family, preferring matching class and science.
- Preserve each equipped item's identity and ownership fields in hero profile responses so weapons remain visible, retain their correct equipment slot, and submit valid inventory IDs when the hero is saved.

### 2026-09-27

- Recharge spent Overdrive from enemy kills, apply equipped and catalyst Overdrive Recharge bonuses, extend active Overdrive from kills, and preserve the meter across reconnects.
- Resume a targeted but idle enemy's action loop when directly attacked so Polaris engages instead of remaining passive.
- Restore the three starting Detail slots, preserve the four-through-six-slot upgrades, and reject hero saves that exceed the unlocked Detail limit.
- Alert an enemy and its immediately adjacent group when a ranged basic attack targets it, even when the group has not entered its normal awareness range.
- Restore Tree of Life's authored lifetime so its healing explosion and disappearance play at expiry and the ability becomes available again.
- Restore Voltroid discharge visuals by sending their authored zap as a one-shot source-to-target beam instead of a persistent attached effect.
- Save campaign squad edits when build 103 submits an all-zero placeholder for an Arena squad that does not exist yet, while retaining explicit clearing for an existing Arena squad.
- Accept build 103's namespaced inventory part identifiers when creating item details so owned equipment resolves to its stored part.
- Keep Citadel Special Two's close-range melee pursuit active instead of restarting its locomotion on every server poll.

### 2026-09-26

- Add a compact credits dialog to the Dark Spin launcher wordmark with a Darkspin website link, the project model note, and callouts
- Stop automatic Sync Snapshot capture from treating normal hero movement toward the server-issued goal as position divergence, and suppress repeat archives for an already captured object incident in the same session.
- Add a private `/taunt` reply explaining that taunt animations were introduced in a newer Darkbuild than Darkspin supports, and correct its command-help listing.
- Restore mission mouse-wheel zoom from a fixed distance to the native minimum through the mission's default distance, retaining the selected zoom while moving and switching heroes.
- Restore Sage's Enrage self-cast animation and buff metadata, use the ally-cast animation for companions, and cap regeneration at the recipient's maximum health.
- Change boss music to normal horde combat music when the boss dies by clearing the active boss flags immediately, while preserving encounter completion and Beam Out timing.
- Keep Wraith's Lifeforce Siphon channel bar active for its six-second drain window and preserve release-to-stop input, separating channel timing from cooldown reduction and the end animation.
- Populate ordinary enemies before mission exploration without arrival effects, and reserve the generic beam-in animation and spawn sound for campaign and tutorial horde waves.
- Restore continuous basic attacks while the right mouse button is held by removing a diagnostic input reset that incorrectly treated a released left mouse button as the end of the attack hold.
- Change Sage's Tree of Life from five repeated growth stages and a six-second lifetime to one full-growth effect and an eight-minute lifetime, playing its disappearance effect only at expiry and retaining the tree after a hero falls.
- Hold mission equipment pickups until successful completion, forfeiting them on abort or squad defeat and preventing reconnects or checkpoint restoration from recovering lost loot.
- Change Space Barracudas from overlapping teleport loops to one opening blink followed by ranged attacks, choosing reachable landings around the target with a preference for its rear.

- Correct enemy damage scaling from stat plus 1 to the authored stat minus 8, reducing inflated early-campaign hits while preserving hero damage.
- Preserve separate current and maximum health and energy through reconnects, checkpoint restoration, and party snapshots so recovered bars do not exceed 100% and keep flashing.
- Make sphere teleporters transfer heroes immediately once their collision footprint fits inside, and keep each gate inactive while enemies remain within roughly three Blitz collision lengths.
- Keep destroyed Gravitic Regulator bases visible and solid for the mission, including after reconnects and checkpoint restoration.
- Gate catalyst drops, pickups, and use behind campaign unlock progression and reduce obelisk catalyst drops from a 75% base chance to 5% while retaining equipment rewards.
- Move Ride the Lightning's teleport and arrival animation from immediate movement and late recovery to the authored impact time, and allow cursor-directed teleports to walkable ground without an enemy target or walking path.
- Restore mission spawning and hero switching from the short teleporter animation to the authored beam-in landing sequence, with matching effects and sound cues, consistent timing, and a brief input lock through landing.
- Save campaign squad edits when the unused Arena deck field is blank instead of rejecting the complete deck update and restoring Blitz, Sage, and Wraith.

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
- Replace raw and Darkaudio registry literals with packed string, resource, reference, and usage indexes, reducing the embedded registry from about 6 MiB to 1.49 MiB without compression while preserving searchable conversion metadata.
- Change Darkrun-generated audio aliases, including registry-backed names, from the `ds_` prefix to a trailing `~` so inferred filenames are immediately distinguishable from authored names.
- Show the Darkrun build version at the start of root and subcommand help output, including bare general invocation.
- Decode structurally verified XAS0 resources with their channel-interleaved frame layout, including shortened final frames, instead of misreading them as garbled XAS1 audio or preserving them as raw SNR files.
- Consolidate directly suffixed indexes, `_vN` versions, numbered variants, and numbered loop families into one normalized folder and family DSE while preserving each WAV definition and its reconstruction metadata.
- Reduce successful Darkrun conversion output to the elapsed time without repeating source and destination paths.
- Treat `darkrun convert <source> .` like an omitted destination so conversion writes the default local `.ds` directory or package.
- Give every retail WAV a pinned human-readable `~` alias without runtime lookup, remove hash identity suffixes from aliased files, and annotate DSE files with searchable audio-event, animation, package, and resource references for reconstruction.
- Trace Darkaudio events through shipped animation, noun, level, UI, effect, and pre-baked resources, pin exact names and resource-owner contexts into searchable conversion metadata, and replace ambiguous unresolved audio trees with resource-role and event-identity folders.

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
