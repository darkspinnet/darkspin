# Lua file index

Each `local/` file is a runnable scenario. Files under `definitions/` describe the Lua API.

## Utility

- `inspect.lua` — Reads supervisor state without launching a game client.

## API definitions

- `definitions/ability.lua` — Ability input, cast results, and observations.
- `definitions/cinematic.lua` — Cinematic control and state.
- `definitions/client.lua` — Client handle and core operations.
- `definitions/debug.lua` — Debugger pause, resume, and state.
- `definitions/diagnostic.lua` — Captures and diagnostic artifacts.
- `definitions/error.lua` — Scenario error results.
- `definitions/hero.lua` — Hero and squad observations.
- `definitions/mission.lua` — Mission selection, entry, and completion.
- `definitions/native.lua` — Native client gameplay observations.
- `definitions/npc.lua` — Controlled NPC operations and snapshots.
- `definitions/participant.lua` — Scenario participant handles.
- `definitions/party.lua` — Party membership and invitations.
- `definitions/profile.lua` — Isolated profile creation and launch.
- `definitions/pvp.lua` — PvP queue, acceptance, and match state.
- `definitions/scenario.lua` — Scenario runtime, parallel work, and assertions.
- `definitions/scene.lua` — Client scene transitions.
- `definitions/screen.lua` — Screen observations.
- `definitions/server.lua` — Owned server setup and fixtures.
- `definitions/session.lua` — Supervisor session state.
- `definitions/ui.lua` — Native UI input and inspection.
- `definitions/window.lua` — Client window operations.
- `definitions/world.lua` — World, hero, NPC, and movement fixtures.

## Local scenarios

- `local/ability_family.lua` — PvE chain and healing channel ability regression.
- `local/camera_ability_cancel.lua` — Movement camera after cancelling Ride the Lightning.
- `local/camera_follow.lua` — Native camera follow during a mission route.
- `local/camera_held.lua` — Camera behavior during held movement and a hero switch.
- `local/cinematic_toggle.lua` — Opening movie and in-game cinematic settings.
- `local/controlled_npc.lua` — AI-disabled NPC movement and combat commands.
- `local/coop_combat.lua` — Two-client native co-op combat in mission 1-1.
- `local/coop_gear.lua` — Co-op appearance, equipment stats, and squad switches.
- `local/coop_replace.lua` — Repeated co-op hero replacement with both clients observing.
- `local/electron_collision.lua` — Electron native cast and NPC collision outcome.
- `local/focused_active.lua` — Focused Ride the Lightning native cast phases.
- `local/focused_combat.lua` — Native basic attack damage and target death.
- `local/focused_spawn.lua` — Seeded solo mission boss population.
- `local/hero_switch.lua` — Native target defeat followed by a squad switch.
- `local/import_editor.lua` — Import Editor entry and visual capture.
- `local/missile_barrage_render.lua` — Missile Barrage native rendering and cast.
- `local/mission_cinematic.lua` — Mission victory, Beam Out, and ship cinematic.
- `local/mod_editor.lua` — Mod creature editor entry and Body palette capture.
- `local/nocturna_wall.lua` — Nocturna Wanderer wall placement and collision.
- `local/noun_zoo.lua` — Two clients fighting selected installed NPC nouns.
- `local/pve_bulk.lua` — Co-op hot-swap sweep of castable ability assets with per-case evidence.
- `local/pvp_1v1_noparty.lua` — Native 1v1 queue without a party.
- `local/pvp_1v1.lua` — Native party invitation and 1v1 queue.
- `local/pvp_2v2.lua` — Four-client 2v2 rounds and results.
- `local/pvp_area.lua` — Arena area/support casts and Time Lapse.
- `local/pvp_beam.lua` — Zetawatt Beam in an owned 2v2 match.
- `local/pvp_bulk.lua` — Multiple owned duels for batched ability observations.
- `local/pvp_combat.lua` — Arena basic, active, and support combat inputs.
- `local/pvp_comet_swap.lua` — Celestial Comet across caster and recipient swaps.
- `local/pvp_comet.lua` — Celestial Comet native push observations.
- `local/pvp_electron.lua` — Electron arena cast and target outcome.
- `local/pvp_enrage.lua` — Enrage and Tree of Life healing.
- `local/pvp_family.lua` — Four-client chain, heal, capacity, and cancellation abilities.
- `local/pvp_flak_projectile.lua` — Flak Cannon with friendly and hostile projectiles.
- `local/pvp_flak.lua` — Flak Cannon area effects and rank fixtures.
- `local/pvp_gear.lua` — Equipped versus bare Arena combat.
- `local/pvp_healing_projectile.lua` — Arena healing projectile behavior.
- `local/pvp_hot_swap.lua` — Arena hero replacement and resource resets.
- `local/pvp_knockout.lua` — Two-round Arena knockout.
- `local/pvp_missile.lua` — Arena Missile Barrage cast.
- `local/pvp_opening.lua` — Arena loading, countdown, and combat entry frames.
- `local/pvp_projectile.lua` — Arena projectile flight and damage.
- `local/pvp_ranked.lua` — Dazing Shot rank fixtures in an owned 2v2 match.
- `local/pvp_return.lua` — Results Continue and return to ship.
- `local/pvp_rig.lua` — Native ability effects on fresh installed hero rigs.
- `local/pvp_rounds.lua` — Best-of-three Arena tiebreaker.
- `local/pvp_silence_swap.lua` — Silence/snare lifetime across hero switches.
- `local/pvp_silence.lua` — Silence behavior at authored rank fixtures.
- `local/pvp_slow_stack.lua` — Dazing Shot slow stacking follow-up.
- `local/pvp_slow.lua` — Dazing Shot slow across hero families.
- `local/pvp_souls.lua` — Soul earning, spending, and death cleanup.
- `local/pvp_sphere.lua` — Delaying Sphere entry, exit, and expiry.
- `local/pvp_status_cleanup.lua` — Meteor Strike stun cleanup after recipient death.
- `local/pvp_status.lua` — Meteor Strike stun across hero families.
- `local/pvp_targeted.lua` — Directed bursts and random-target lightning.
- `local/pvp_team_heal.lua` — Four-client Tree of Life ally healing.
- `local/pvp_time_lapse.lua` — Arena Time Lapse cast.
- `local/pvp_toss.lua` — Arena toss ability cast and target outcome.
- `local/pvp_wave.lua` — Arena wave ability cast and target outcome.
- `local/repeat_mission.lua` — Two seeded co-op mission passes on one server.
- `local/scaldron_death.lua` — Real co-op Corruptor defeats, portal gating and timed minion teardown.
- `local/screen_flow.lua` — Ship overlays and native screen selection.
- `local/seed_reset.lua` — Profile reset, squad seed, and fresh accounts.
- `local/social_flow.lua` — Native Social invitation and party flow.
- `local/solo_mission.lua` — Ordinary solo mission 1-1 combat.
- `local/solo_replace.lua` — Repeated solo campaign hero replacement.
- `local/two_arsenal_parallel.lua` — Parallel launch of two Arsenal clients.
- `local/two_arsenal.lua` — Two-client Arsenal authentication and presence.
- `local/two_clients.lua` — Seeded dual-client scenes, party, and mission.
- `local/two_mission.lua` — Two clients entering a shared mission.
- `local/two_party.lua` — Server party fixture and membership.
- `local/two_profiles.lua` — Two isolated profiles and client authentication.
- `local/two_scenes.lua` — Independent ship scene transitions.
- `local/wanderer_parallel.lua` — Eight rolling clients loading Wanderer missions.

