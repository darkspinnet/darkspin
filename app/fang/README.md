# Fang source layout

Fang's bootstrap and build-mode switches live at this directory's root. Each feature subdirectory is a cgo package imported by the bootstrap, so Go compiles its C sources as separate translation units. `fang.h` exposes initialization; `hook.h` forwards only common ABI, patch and trace infrastructure. Feature interfaces belong in their owning directory.

| Directory | Responsibility |
| --- | --- |
| `bug/` | First-chance exception reporting, recent history, bounded snapshot recording, and Arsenal diagnostics |
| `chat/` | Command catalogs and normalization; native actor-dependent chat behavior remains in the bootstrap |
| `cinematic/` | Existing native Escape handling and cinematic preferences |
| `client103/` | Loaded-image identity, named native bindings, calling conventions, signature evidence and verified partial layouts |
| `display/` | Borderless window preferences, taskbar overlay and their native options controls |
| `editor/` | Normal editor item-availability compatibility |
| `hook/` | Shared memory checks, import-table lookup, patch ownership, rollback and installation reports |
| `login/` | Existing native login presentation and failure reporting |
| `mod/` | Mod resource mounting, editor/palette integration, appearance and audio compatibility |
| `network/` | Winsock redirection, party envelopes, network capture and the scenario control transport's original-function access |
| `scenario/` | Development control connection, native input/observation, renderer and selection diagnostics, and capability reporting |
| `shop/` | Shop pricing, inventory availability and shop UI compatibility |
| `zoom/` | Normal gameplay camera zoom compatibility |

The `mod/` root retains its bootstrap and shared interfaces. Native implementations live in `animation/` (emote state and dance/audio coordination), `appearance/` (imported character identity, fit, scale and portraits), `audio/` (sound, music and voice), `editor/` (editor controls, previews and validation), `palette/` (palette pages and selection controls), `resource/` (source-backed streams), `scene/` (scene and environment assets), and `ui/` (shared button styling). Each subgroup is a cgo package imported by the mod bootstrap.

Every C function has a short `//` summary describing its purpose and effects. `Reads` indicates a query or calculation without a persistent or caller-visible write; `Modifies` and `Reads/modifies` identify native operations, patches, Fang state changes, output-buffer writes, resource ownership, or diagnostic output. Existing detailed evidence and inline explanations remain alongside these summaries.

## Native bindings and hook ownership

`client103/image.c` accepts a loaded PE32/i386 client with file version 5.3.0.103, resolves RVAs against its loaded base, checks readable bounds, and reports image identity and named validation failures. `bootstrap.h` names the existing bootstrap function and patch-site RVAs and their calling conventions. These transcribed legacy definitions are not new prologue evidence: existing call-target and signature checks remain necessary. Build 127 is not an implementation target.

Camera and shop are the first feature bindings with separate bind/preflight and install phases. Their catalogs retain explicit build-103 evidence, and small native views have compile-time offset/size checks. Partial views describe only known spans; they do not claim complete recovered classes. Runtime readability checks remain required.

`hook/patch.c` records named patches, expected and original bytes, replacement bytes, original protection, ownership and failures. Feature-owned hook sets support commit and reverse-order rollback; incomplete protection/cache cleanup remains recorded for retry, and rollback refuses to overwrite another owner's changes. Camera zoom and shop use these transactions. Existing patch helpers also pass through this journal, retaining their existing checks and source locations while they await feature-specific migration. This does not make live DLL unloading safe.

Named image-validation and hook-installation records are written to the existing diagnostic history at startup and during requested bug snapshots. Recording remains bounded and preserves existing snapshot loss accounting. No new configuration, gameplay hook, MacroQuest dependency or C++ plugin framework is introduced.

## Camera usage

`zoom/zoom.c` is used by normal and scenario builds. `fang_install` calls `fang_install_camera_zoom` during installation. Its build-103 signature checks protect the existing local mission-refresh call hook. After native refresh, that hook reopens only the collapsed zoom interval from the native minimum to the mission maximum. It preserves the wheel's distance/target when the same camera and level remain active. It does not synthesize input, replace camera position/orientation, or override fixed/scripted cameras. This reorganization does not add or change a gameplay hook.

`scenario/camera.c` owns observation. In explicit `scenario,fangdebug` builds, `fang_control_camera_read` feeds the camera portion of gameplay-state receipts: target, modes, center/desired/anchor positions, hold timer and distances. The scenario gameplay control also requests the trace snapshot. These paths read the validated native cache without invoking the lazy transform rebuild.

The same file retains `fang_trace_camera_state` for ordinary diagnostic bug snapshots as well as scenarios. Its presence in `scenario/` does not make that existing bug-report evidence scenario-only. Read-only bindings in `client103/camera.c` initialize independently of zoom installation, so observation remains available if the zoom-specific checks fail. Observation does not install a second zoom hook. Existing cinematic camera hooks remain owned by the bootstrap's cinematic lifecycle.

## Build modes

Use the existing Mage commands; no separate native build step is required. `mage build` compiles normal diagnostic Fang as part of the application build. `mage scenario:build` explicitly selects scenario support; a `fangdebug` build alone does not enable the control connection or scenario capability export. Release builds retain their existing diagnostic/scenario exclusions.

Conditional `#cgo scenario,fangdebug CFLAGS` adds a native preprocessor define only when both build tags are present; it does not restrict the containing package to scenario builds. Only packages with scenario-specific branches retain that define: the root bootstrap, bug recorder and mod editor integration, plus the scenario package's own explicit build-mode files. Normal zoom and editor compatibility remain available without scenario support.

Mage fingerprints all Fang headers into its C compilation flags because cgo does not track included headers outside a package's own directory. This prevents feature packages from reusing stale native objects after shared ABI or patch API changes. Mage preserves user-supplied `CGO_CFLAGS` and appends the fingerprint automatically.
