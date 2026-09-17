# Changelog

### 2026-09-17

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
