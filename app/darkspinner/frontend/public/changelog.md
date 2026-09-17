# Changelog

### 2026-09-16

- Fix duel lobbies stalling before squad selection by removing malformed character data from initial player updates.
- Generate main artifacts with unstable build numbers and commit labels, permanent download links, and Windows artifact self-updates while keeping GitHub releases exclusive to release.
- Prepare for darkspinnet org public launch

### 2026-09-07

- Change Invincitron's hover drone from remaining at its owner's position to circling above him, with its firing position following the same orbit.
- Restore Nashira's Shadow Panic shriek on her melee/lob attack paths, with timed nearby fear, a fifteen-second cooldown, and normal attacks resuming after the cast.
- Change Nashira illusion deaths from the full boss death animation to a brief duplication-effect disappearance; preserve the real boss's death sequence.

- Fix boss-summoned Exploder Scarabs failing damage and explosion processing because they were incorrectly treated as boss-wave actors.
- Change Laser Tank beams from following heroes to fixed placement anchors, with explicit effect cleanup when firing ends or is interrupted.
