# DarkSpin installation and development

### Dev Setup

- install [git](<https://git-scm.com/install/windows>)
- install [go](<https://go.dev/dl/>)
- install [msys2](<https://github.com/msys2/msys2-installer/releases/download/nightly-x86_64/msys2-x86_64-latest.exe>)
- open msys2 as a terminal prompt, and run `pacman -Syu` then `pacman -S --needed mingw-w64-i686-gcc`
- add to system env variables PATH of `C:\msys64\mingw32\bin`
- if linux, `sudo apt install libgtk-3-dev libwebkit2gtk-4.0-dev`
- run `go install github.com/magefile/mage@latest`
- run `go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0`
- run `mage build` to build everything, a good initial sanity test
- if pnmp fails, run `winget install OpenJS.NodeJS.LTS` then `npm install --global pnpm`


## Application layout

- `app/darkrun` is the headless darkspin executable and owns its Cobra command
  package under `app/darkrun/cmd`.
- `app/darkspin` is the desktop launcher for an external darkspin server.
- `app/darkspinner` is the standalone desktop application that manages local
  authentication, server lifecycle, profiles, patching, and game launch.
- `app/fang` builds the injected `fang.dll` compatibility hook.
  Its feature directories and camera usage are documented in [the Fang source layout](app/fang/README.md).
- `app/palp` builds the storage-independent `wasmauth.wasm` authentication
  bridge.

Reusable server behavior remains under `server`; executable command wiring
belongs to the application that consumes it.

The root `patcher` package owns manifest loading, patch planning, safe file
replacement, and structured progress dispatch. DarkSpin and DarkSpinner adapt
those events into their own UI state without introducing a Wails dependency
into the patch engine.

The root `window` package owns Windows process discovery, termination, taskbar
restoration, and single-instance replacement. Desktop applications provide
their executable names and dialog text without duplicating Win32 operations.

## Headless container deployment

The headless server requires PostgreSQL for player storage and runs account login and gameplay together in one process. Provide a readable build-103 installation and keep it available for content rebuilds; startup creates its internal TOML settings, authentication secret, and missing PostgreSQL tables. A separate `darkrun auth` process is not needed. New Darkrun configurations require account passwords and default to loopback. On a native host, provision a PostgreSQL database and role with table-creation permission, then export its connection settings before starting:

```sh
export PGHOST=database.example PGDATABASE=darkspin PGUSER=darkspin
export PGPASSWORD='replace-with-your-password' PGSSLMODE=verify-full
DARKSPIN_ADVERTISED_HOST=192.0.2.10 DARKSPIN_GAME_PATH=/path/to/Game darkrun server --dedicated
```

Replace `192.0.2.10` with the IPv4 address players can reach. Later starts reuse saved network settings, but keep `DARKSPIN_GAME_PATH` set on every invocation (or use the installation beside the executable). Environment settings use the existing CLI parsing and saved configuration, with precedence **explicit CLI flags → nonempty environment settings → persisted settings or defaults**. Empty environment settings are ignored; reset optional saved settings explicitly with CLI flags, such as `--content-path=""` or `--bind-host=""`.

| Environment setting | Equivalent flag | Purpose |
| --- | --- | --- |
| `DARKSPIN_ADVERTISED_HOST` | `--advertised-host` | IPv4 address players can reach; saved internally. |
| `DARKSPIN_GAME_PATH` | `--game-path` | Required installed build-103 client used to rebuild missing or stale `content.db` and web assets automatically on startup; the installation is read-only. |
| `DARKSPIN_CONTENT_PATH` | `--content-path` | Native generated-cache directory containing `content.db` and `www/static`; Compose fixes this to `/darkspin/runtime/darkspin/cache`. |
| `DARKSPIN_CONFIG_PATH` | `--config` | Persistent settings file; player saves and diagnostics live in its sibling `darkspin` directory. |
| `DARKSPIN_BIND_HOST` | `--bind-host` | Listener IPv4 address; saved internally. |
| `DARKSPIN_PORT` | `--port` | Shared TCP/UDP port, from 1 to 65534; saved internally. |
| `DARKSPIN_IS_DEDICATED` | `--dedicated` | Password-required login mode (`true` or `false`); saved internally. |

The game path is only a preparation source, never a destination for writes. Darkrun and Darkspinner call the same content preparation routine: valid caches are reused, while missing or stale caches are rebuilt from this path before server listeners start. Keep the source mount available across upgrades for automatic rebuilds. Headless startup and update preflight validate the source version and required package indexes even when caches are current; copied caches cannot replace the required installation. A failed database import leaves the previous database in place and stops startup. Relative content-path options are saved as absolute paths. Config and content paths remain internal TOML-backed settings where applicable; environment variables provide startup inputs rather than a separate configuration store.

The server holds an operating-system lock for its runtime through preparation, startup and shutdown. Another correctly configured `darkrun server` invocation for that runtime reports that an instance is running or starting and exits without changing its settings or opening another database. This is an ownership check, not a readiness or gameplay check. Stop the owner before changing startup settings or using `--prepare-only`. The lock file may remain after exit; the operating system releases the lock when the process exits. Keep one server service per runtime and PostgreSQL player database.

Add `--prepare-only` to generate settings and prepare or verify content without opening server listeners. The same setup fields are editable through Darkspinner's Config interface. Connect from another Darkspinner using its Remote Servers account flow; dedicated mode requires a password even for loopback connections. Existing passwordless local profiles must not be used as dedicated login credentials.

When reusing an existing local configuration, explicitly set `DARKSPIN_IS_DEDICATED=true` once to switch it to password-required login. A new configuration enables this automatically. Local preparation and serving auth/game traffic run in the same process; no separate `darkrun auth` service is needed.

## Downloadable Compose setup

Use the [Compose setup guide](deployment/README.md) for the [unstable setup ZIP](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-compose-unstable.zip), required game mount, PostgreSQL initialization, and persistent storage. Stable setup is not available yet.

Compose always provisions PostgreSQL with a persistent named volume and starts `darkrun bootstrap` after database readiness. The container downloads and verifies its initial binary into the writable runtime mount. Bootstrap supervises the server, captures a compressed `pg_dump` alongside stopped-runtime file backups, and restores the matching database and files if an update fails its first boot. The infrastructure image includes PostgreSQL 18 client tools.

Mount the complete `darkspin/` directory containing `bin/darkrun`, `runtime/`, and `backup/`. Configuration lives at `runtime/darkspin.toml`; live PostgreSQL data lives in its separate named volume, and completed snapshots include `backup/<date>/<snapshot>/postgres.dump`. The game installation remains a required read-only mount at `/game`. See the guide before migrating an older runtime layout; existing SQLite players are not imported automatically.

## Packaging and source development

Package the standalone setup without building application binaries:

```sh
mage darkrun:compose
```

This creates `bin/deployment/darkrun-compose-v<version>.zip` and an allowlisted bundle containing `compose.yaml`, `.env.example`, `README.md`, and `VERSION`. It excludes local settings, binaries, backups, game assets and saves. `DARKSPIN_BUILD_VERSION` sets the bundle version; `DARKSPIN_COMPOSE_IMAGE` optionally sets the infrastructure image reference.

`main-build` publishes the setup ZIP and Linux binary/manifest artifacts. The manifest pins the executable's SHA-256 and immutable Actions artifact URL. A separate `bootstrap-image` workflow publishes the infrastructure image only when its inputs change or it is manually dispatched.

For local infrastructure development:

```sh
cd deployment
docker compose -f compose.yaml -f compose.build.yaml build darkrun
```

The deployment Dockerfile installs only download tools and the initial-install entry point. Its build context is `deployment/`, allowlisted to the Dockerfile and entry point. Application source is not compiled into this image. Container startup, updates and multiplayer behavior require runtime validation; source and packaging checks alone do not establish readiness.

## Run

```sh
mage build

mage darkspin:build
mage darkspin:auth
mage darkspin:server

mage darkspinner:build
mage darkspinner:run
mage darkspinner:buildrun

mage darkspin:build
mage darkspin:run
mage darkspin:runNormal

mage tutorial:run
mage tutorial:run2
mage tutorial:debug
```

`mage build` is the aggregate verification build. `mage darkspin:server` rebuilds
`bin/server/darkspin.exe` and then
runs the server with `bin/server` as its working directory. That directory also
contains `darkspin.toml`, server data, and the generated `darkspin` runtime
directory.
The Mage target supplies the repository-local `bin/game` installation. Normal
server and launcher runs do not write a continuous server protocol trace.
Use `darkrun server --trace server.jsonl` for an explicit capture beneath
`darkspin/logs/traces`; Lua diagnostic sessions and `mage tutorial:debug` also
request protocol captures. Sync Snapshot's rolling gameplay history and `/bug`
capture do not depend on that file. The protocol trace adds HTTP, Blaze, and QoS
metadata that is distinct from the snapshot's gameplay traffic.
Generated IDA and game reverse-engineering logs belong under `bin/game/logs`.

`mage darkspin:auth` rebuilds darkspin and runs `darkspin auth --local` from `bin/server`.
The auth broker and game server read the same `[auth]` settings from
`bin/server/darkspin.toml`.

The DarkSpinner all-in-one Wails application builds into `bin/game`, beside `DarksporeBin` and `Data`,
with its own server data, configuration, saves, cache, logs, and traces. It manages local auth,
the darkspin server, retained player profiles, patch readiness, and game launch.

Launcher preferences share `darkspin.toml` with server settings. Skip-intro and
skip-cinematic choices persist independently as `is_intro_skipped` and
`is_cinematic_skipped` in `[launcher]` and are restored on startup. Explicit
`--skip-intro` and `--skip-cinematic` flags override the saved choices for that
run without changing them. The Wine runtime
selection is stored in `[launcher.wine]` as `runtime_path`; the Spore installation
and detection flag are stored in `[launcher.mod]` as `spore_path` and
`is_detection_attempted`. Older `darkspin/launcher.toml` and root `launcher.toml`
files are merged automatically, with existing `darkspin.toml` settings taking
precedence. The old file is deleted only after the combined configuration is
successfully saved. Server configuration saves preserve the launcher tables.

```sh
mage darkspinner:build
mage darkspinner:run
mage darkspinner:buildrun
```

On Windows the application is `darkspinner.exe`; Linux uses the native `darkspinner` executable, and macOS uses `darkspinner.app`. Place the launcher beside the game installation. On macOS the runtime configuration, cache, saves, and game paths resolve beside the `.app` bundle, outside its contents. Linux and macOS launch the Windows game through the existing Wine startup proxy. They require a Wine environment capable of running the 32-bit x86 game; an ARM64 launcher does not supply x86 emulation. macOS bundles are not notarized, and Linux/macOS updates are manual.

`mage darkspinner:run` starts the existing native application without rebuilding, with cinematics skipped for development. Use `mage darkspinner:buildrun` to rebuild and launch, or `mage darkspinner:build` to build only. Direct and CI launches play cinematics unless the player's saved setting skips them.

Both workflows build six targets on native OS runners. The `windows-win32` download uses Go's `386` architecture; all other suffixes use Go's OS/architecture names. The Windows amd64 launcher embeds a small x86 loader helper so Fang still loads into the 32-bit game. Fang remains x86 on every platform. `DARKSPINNER_GOARCH` selects the launcher architecture for `mage darkspinner:buildcinative` (for example `amd64`, `386`, or `arm64`). Local Windows builds default to `386`; Linux and macOS default to the host architecture.

Merge into `release` after bumping `releaseSemver` in `magefile.go` to publish a stable `v<version>` release. Stable asset names omit the version; manifests pin downloads to their exact release tag. A version such as `1.1.0-rc.1` is a prerelease and does not advance the latest stable feed. The workflow publishes ZIPs and Windows update manifests, with no standalone executable asset.

Pushes to `main` create unstable Actions artifacts only, with no GitHub release or tag. Versions such as `1.0.0-unstable.42.1+gabcdef123456` identify the workflow run, attempt, and commit without manual version bumps. The launcher displays the full version. New pushes cancel unfinished runs; the latest-successful download links advance when the entire workflow succeeds.

These permanent download links become available after the first successful run of the corresponding updated workflow:

| Target | Latest stable release | Latest unstable main artifact |
| --- | --- | --- |
| `windows-amd64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-windows-amd64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-windows-amd64-unstable.zip) |
| `windows-win32` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-windows-win32.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-windows-win32-unstable.zip) |
| `darwin-amd64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-darwin-amd64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-darwin-amd64-unstable.zip) |
| `darwin-arm64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-darwin-arm64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-darwin-arm64-unstable.zip) |
| `linux-amd64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-linux-amd64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-linux-amd64-unstable.zip) |
| `linux-arm64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-linux-arm64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-linux-arm64-unstable.zip) |

The Windows unstable artifact contains `darkspinner.exe` directly. Linux and macOS artifacts contain an inner ZIP that preserves executable permissions and macOS bundle structure; extract that inner ZIP as well. Unstable links use [nightly.link](https://nightly.link/), require a public repository for anonymous downloads, and expire with the 14-day artifact retention period if no new successful build replaces them.

Darkrun's standalone server/CLI is also packaged for Windows and Linux amd64, using the same stable version or unstable run version as Darkspinner. Build locally with `mage darkrun:buildci windows` or `mage darkrun:buildci linux`; archives are written beneath `bin/darkrunci/<os>-amd64`. Each ZIP contains only `darkrun.exe` or `darkrun`, with no game assets or runtime data. Unstable artifacts contain an inner ZIP, which must also be extracted. Darkrun updates are manual.

| Darkrun target | Latest stable release | Latest unstable main artifact |
| --- | --- | --- |
| `windows-amd64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkrun-windows-amd64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-windows-amd64-unstable.zip) |
| `linux-amd64` | [Download ZIP](https://github.com/darkspinnet/darkspin/releases/latest/download/darkrun-linux-amd64.zip) | [Download artifact](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-linux-amd64-unstable.zip) |

Windows self-updates stay on their installed channel and architecture. They compare versions and verify the extracted executable's SHA-256 before replacement; failed checks are logged and startup continues. Each unstable manifest pins its binary download to a specific artifact ID, so another run cannot change an in-progress download. The unstable manifest artifact contains `darkspinner-update.json`.

| Windows target | Stable manifest | Unstable manifest ZIP |
| --- | --- | --- |
| `windows-amd64` | [JSON](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-update-windows-amd64.json) | [ZIP](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-update-windows-amd64-unstable.zip) |
| `windows-win32` | [JSON](https://github.com/darkspinnet/darkspin/releases/latest/download/darkspinner-update-windows-win32.json) | [ZIP](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkspinner-update-windows-win32-unstable.zip) |

The original `darkspinner-update.json` stable endpoint and `darkspinner-update-unstable` artifact remain x86 aliases for existing installations. `DARKSPIN_BUILD_VERSION` and `DARKSPINNER_UPDATE_URL` are build-time overrides; ordinary builds use the source version and stable endpoint. Merging main into `release` merges source changes, not tags. Publishing a different stable commit requires a fresh stable version.

Artifacts are also available from the successful run's Artifacts section or through authenticated GitHub CLI downloads:

```powershell
$runId = gh run list --repo darkspinnet/darkspin --workflow main-build.yml --branch main --status success --limit 1 --json databaseId --jq '.[0].databaseId'
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($runId)) { throw 'No successful main build found.' }
gh run download $runId --repo darkspinnet/darkspin --name darkspinner-windows-amd64-unstable --dir downloads
```

Substitute any target from the table in the artifact name. The selected artifact must still be retained.

A fresh
`bin/game/darkspin/saves/darkspin.db` starts without development seed users; create a
character in the Profile section or select an existing retained profile.
Closing DarkSpinner cancels active launcher work, terminates the client
it launched, shuts down local auth and the game server, and waits briefly for
their background routines to exit before the desktop application closes.
DarkSpinner is single-instance. Starting it again restores and focuses an existing
taskbar window. If the existing process has no taskbar window, a native Windows
prompt offers to close that process cleanly and start the new instance.

The repository does not contain the game/server asset payload. Before running
the server, copy the required assets into `bin/server`. The expected layout
includes `bin/server/data` and the creature template JSON files under
`bin/server/data/creature`. Web pages, styles, scripts, fonts, and images
under `server/assets/www/static` are embedded into `darkspin.exe`. Asset acquisition
instructions will be documented separately.

The same build also writes 32-bit `bin/game/darkspin.exe` and
`bin/game/fang.dll`.
The DLL redirects game hostnames to `localhost` and HTTP port 80 to the shared
service port 42127.
The launcher loads the Mage-built `fang.dll` beside `darkspin.exe`. Run:

```sh
mage tutorial:run
mage tutorial:run2
```

`darkspin.exe` is a Wails/Vue patcher that owns update status, desktop OAuth, and
the final game handoff. `mage tutorial:run` opens it for the primary local
profile; `mage tutorial:run2` opens it for the secondary local profile. Both targets request
auto-play with `--skip-cinematic`: after authorization and any configured patch,
the patcher starts the game suspended, injects `fang.dll`, initializes its
network and certificate hooks, and resumes the game. They do not
build or start the server or local auth service. Run `mage build`,
`mage darkspin:server`, and `mage darkspin:auth` separately. `mage darkspin:run`
remains the generic launcher without an account. Like `mage tutorial:run` and
`mage tutorial:run2`, it starts the existing binary and never rebuilds it.
Use `mage build` explicitly after changing launcher code. Additional positional
arguments are forwarded to game.

Set `darkspin_PATCH_URL` during `mage build` to bake a YAML patch-manifest URL
into distributed copies of `darkspin.exe`. The manifest supports `downloads`,
`deletes`, an optional `downloadprefix`, file sizes, and MD5 verification. An
unset URL disables remote patching while retaining OAuth and Play.

`pnpm` is required for `mage build`. The build fails when `pnpm` is unavailable
instead of using stale generated frontend output. Mage installs the locked
dependencies and regenerates `app/darkspin/frontend/dist` before Wails embeds
it; the generated directory is intentionally ignored by Git.

Linux hosts also require the 32-bit MinGW-w64 C compiler
`i686-w64-mingw32-gcc` to cross-compile `fang.dll`. On Debian and Ubuntu it is
provided by the `gcc-mingw-w64-i686` package. Native DarkSpinner additionally
requires Wine, GTK 3, and WebKitGTK 4.0. Debian and Ubuntu development hosts can
install `wine`, `libgtk-3-dev`, and `libwebkit2gtk-4.0-dev`.

Every fang.dll run bypasses the EAWebKit
`SINGLEPLAYER / MULTIPLAYER / EXIT` bootstrap screen by applying the native
`playCurrentApp` result as soon as game initializes its launcher bridge.
This is unconditional because darkspin supports the multiplayer server path;
`--skip-cinematic` is not required for this behavior.
The legacy `bootstrap/launcher` HTML and images are not shipped. The server
keeps an empty compatibility endpoint for the URL advertised to build 103,
while fang.dll exits the native bridge before any page is needed.

`mage darkspin:run` enables `--skip-cinematic` by default. Use
`mage darkspin:runNormal` to
start the client without launcher arguments and preserve the original cinematic
flow. `--skip-intro` bypasses only the EA and Maxis startup videos.
`--skip-cinematic` includes that startup bypass, stops scripted VP6 playback
through the engine's native movie-player skip operation, and skips the
post-login 3D ship-camera tour through the engine's native spline completion
path. It also selects the bridge's `START` action through the native spaceship
navigation callback immediately after safely completing the ship tour. It then
completes the follow-up camera transition and continues directly into the
Arsenal/squad screen without waiting for input. Omitting both flags preserves
the original flow. The first-play prologue and tutorial introduction run even
when cinematic skipping is enabled.

Mage and the launcher use the repository-local playable installation at
`bin/game/<Game>Bin/<Game>.exe`. Its game data remains alongside it at
`bin/game/Data`. Mage passes `bin/game` to the standalone server so both sides
resolve the same version and assets. `mage darkspin:server`, `mage darkspin:run`,
and `mage tutorial:debug` all use this layout. An explicit `--game-exe` overrides it;
`GAME_EXE` is only a fallback when the local executable is absent.

By default, `fang.dll` is loaded from the directory containing `darkspin.exe`.
An explicit `--fang` path is the only override; DLLs beside the selected game
executable are not considered.

The `server` command detects the game version from
`<GAME>Bin/version_bin.txt`, loads or generates `darkspin.toml`, initializes
the external server assets and Lua VM, and shuts down cleanly on SIGINT or
SIGTERM. The documented first-run template is tracked at
`server/assets/darkspin.toml` and embedded in the executable, so a missing runtime config
is recreated with its comments intact.

All advertised services share one configurable TCP/UDP port:

```toml
[server]
host = "127.0.0.1"
port = 42127
world_player_limit = 25
```

`world_player_limit` caps simultaneously authenticated users across the server;
it is separate from game's four-player limit for an individual Blaze game
instance. The default is 25. A PostgreSQL deployment can raise
the value explicitly after sizing the database and server host.

Accepted chat messages are stored in the selected player database's `chat_log` table and emitted as
JSON Lines to stdout by default; no separate chat log file is written:

```toml
[chat]
is_stdout_enabled = true
```

Set `is_stdout_enabled = false` to disable stdout output. The database audit
remains enabled with either SQLite or PostgreSQL. Legacy `file_path`
settings are ignored.

Persistence uses `darkspin/saves/darkspin.db` for mutable player state and
`darkspin/cache/content.db` for reference content. SQLite uses WAL mode, foreign keys,
and transactional profile updates; schemas are rebuilt directly without
migration/version bookkeeping during the current destructive development phase:

```toml
[storage]
driver = "sqlite"
```

`darkrun server` requires PostgreSQL and dedicated mode. Set `DARKSPIN_POSTGRES_URL`, or supply the standard `PGHOST`, `PGDATABASE`, and `PGUSER` variables together. `PGPORT`, `PGPASSWORD`, `PGSSLMODE`, `PGSSLROOTCERT`, and `PGCONNECT_TIMEOUT` configure the connection. Missing or partial settings and connection failures stop startup rather than falling back. Connection credentials are never saved to TOML, and desktop local play remains on SQLite.

PostgreSQL stores player profiles, statistics, chat, and mission checkpoints; `content.db` remains local SQLite. Switching storage does not migrate existing data. Use one active server per player database. Linux `darkrun bootstrap` supports scheduled file-plus-`pg_dump` snapshots, binary updates, and matching database restoration. It requires `pg_dump`/`pg_restore`, access to the `postgres` maintenance database, and ownership plus `CREATEDB` (or a superuser) for rollback. Direct `darkrun server` does not schedule backups. See [the Compose guide](deployment/README.md#scheduled-backups-updates-and-recovery) for configuration and recovery details.

Compose always provisions PostgreSQL. `POSTGRES_DB`, `POSTGRES_USER`, and required `POSTGRES_PASSWORD` configure both PostgreSQL initialization and Darkrun's connection. Compose persists PostgreSQL 18 in a named disk volume and waits for database readiness; Darkrun creates its tables at startup. Initialization settings only apply to an empty volume. Ordinary container replacement and `docker compose down` retain data; `down -v` deletes the database volume. SQLite player storage is reserved for desktop local play.

Runtime layout is intentionally implicit: SQLite uses
`darkspin/saves/darkspin.db` and `darkspin/cache/content.db`, web content is
served from the executable, and playable
creature definitions are loaded from `content.db`. The legacy creature-template
JSON files are not startup requirements.

The optional research database is built explicitly and is never generated by
normal server startup:

```sh
darkspin build meta
```

When `darkspin.exe` is placed in a vanilla Game root, the command reads its
sibling `DarksporeBin` and `Data` directories and writes
`darkspin/cache/meta.db`. Use `--game-path` and `--output` for development
layouts. The command refuses to overwrite an existing database and verifies
SQLite integrity, foreign keys, package counts, stored payload hashes, all
1,029 Lua bytecode resources, and all 2,407 compiled animation resources before
installing the result.

DarkSpinner must be placed in the Game installation root beside the
`DarksporeBin` and `Data` directories. Use `darkspinner.exe` on Windows and the
native `darkspinner` binary on Linux. Official builds embed `fang.dll`; Linux
builds also embed a Wine startup proxy. The proxy is placed temporarily at
`DarksporeBin/VERSION.dll`, loads Fang inside the game process, and is removed
when the game exits. A marked proxy left by a crash is removed on the next
DarkSpinner startup; an unrelated `VERSION.dll` is never overwritten.

Use the built-in database command for focused inspection and updates. It reads
the configuration and resolves the requested table to either `darkspin/saves/darkspin.db` or `darkspin/cache/content.db`. With a dedicated configuration and PostgreSQL environment settings, `darkrun db` routes player tables to PostgreSQL and content tables to local SQLite. Use the same environment as the running server:

```sh
darkspin db user get 1
darkspin db user get darkspin
darkspin db user get login_name=darkspin
darkspin db user get id ">=" 2
darkspin db user set display_name "Go Dark" where id=1
```

For `get`, a numeric shorthand queries `id` exactly and other shorthand values
perform a partial `login_name` match. Explicit predicates support `=`, `!=`,
`<`, `>`, `<=`, and `>=`; multiple predicates are combined with `AND`. `set`
always requires an explicit `where` clause. Results are written as formatted
JSON, and `get` is limited to 100 rows by default (`--limit` accepts 1–1000).
Use `--config` when the configuration is not in the current directory.

Useful overrides:

```sh
go run . server \
  --config darkspin.toml \
  --blaze-cert server.crt \
  --blaze-key server.key
```

`--blaze-cert` and `--blaze-key` must be supplied together. Without them the
Blaze listeners use plain TCP, which is convenient for local protocol work.
The old embedded private key and obsolete SSLv3/RC4 configuration are not
carried into this port.

The local all-in-one topology uses port 42127 for every advertised service. A
TCP multiplexer routes HTTP and Blaze connections while QoS binds UDP 42127,
which can coexist with TCP on the same numeric port. The client historically
uses UDP 3659 as its source port, so the server does not bind that source port.

`mage tutorial:debug` is the paired developer orchestrator. It starts `darkspin.exe server`
with `bin/server/darkspin/logs/traces/server.jsonl`, waits for the local API to become ready,
then starts the client with `bin/server/darkspin/logs/traces/client.jsonl`; the server it
started is stopped when the client exits. The injected DLL records client socket
and alert activity, reapplies those hooks when EAWebKit loads, and applies the
build-specific local Blaze compatibility patch. The server trace records
decoded HTTP, Blaze, and QoS events. Blaze events
include redacted TDF field shapes (labels, types, and empty markers), never
credential or payload values. These traces are intended to identify the next
client request without guessing protocol behavior.

`darkspin --trace <file>` records the client-side socket/alert stream. The more
explicit `--client-trace` name is an alias. Server options belong to
`darkspin.exe server`; run `darkspin.exe server --help` for its usage.

## JWT launch login

The 5.3.0.103 launcher can establish a Blaze session with a short-lived JWT
instead of placing an account password in the game. The desktop auth broker and
game server share these settings in `darkspin.toml`:

```toml
[auth]
jwt_secret = "replace-with-at-least-32-random-bytes"
jwt_issuer = "darkspin-web"
jwt_audience = "darkspin"
```

The generated local configuration contains a development secret; replace it
before exposing a server beyond localhost. A token must use HS256, have a valid
`exp`, match the configured issuer and audience, and identify an existing local
account with `email` or `sub`. Fang waits for the native login manager,
submits the reserved identity `token@local.invalid` with the JWT, and then
clears its copy. The server exchanges it for the normal internal session token
used by later Blaze and HTTP requests.

The desktop authentication service URL is compiled into `darkspin.exe`; there is
no runtime `--auth-url` override. Local builds default to
`http://127.0.0.1:8090`. Set `darkspin_AUTH_URL` while running `mage build` to
produce a launcher for another service:

```powershell
$env:darkspin_AUTH_URL = "https://accounts.example.com"
mage build
```

The equivalent direct Go build uses the link-time variable:

```powershell
go build -ldflags "-X github.com/darkspinnet/darkspin/app/darkspin.authServiceURL=https://accounts.example.com" -o bin\game\darkspin.exe .\app\darkspin
```

At runtime, `darkspin.exe --account player@example.com` always contacts the URL
baked into that launcher.

For development, launch a token directly or place the raw token in a `.dsjwt`
file:

```powershell
bin\game\darkspin.exe --jwt "header.payload.signature"
bin\game\darkspin.exe C:\path\login.dsjwt
```

`--jwt` exposes the credential in the launcher command line and should only be
used with very short-lived tokens. The launcher also accepts a future website
protocol invocation:

```text
game://login?token=header.payload.signature
```

Windows URI and `.dsjwt` file associations are deliberately not installed by
the build. An installer can register either association with `darkspin.exe` later.
Because browsers and Windows pass protocol URLs on the command line, production
web integration should put a short-lived, single-use launch credential in the
URI rather than a reusable account token.

The default local configuration consolidates the redirector and main Blaze
service on TCP 42127. A traced 5.3.0.103 login confirmed that both connections
use the same Blaze framing and local plaintext compatibility path. Explicit
legacy configurations may still assign separate ports; coincident Blaze ports
are deduplicated into one listener.

The current traced 5.3.0.103 bootstrap completes Login, LoginPersona, PostAuth,
UserAdded/UserUpdated notification handling, network-info, token, rooms, and
presence initialization, and reaches the rendered 3D client scene. Multiplayer
matchmaking and connected RakNet gameplay remain the next implementation
boundary.

## Implemented subsystems

- legacy game configuration
- delayed task scheduling and clean service lifecycle
- Blaze framing, recursive TDF, registry, TLS, authentication, user sessions,
  redirector, utility, rooms, associations, messaging, playgroups, and game
  manager components
- normalized transactional SQLite users, squads, creatures, parts,
  settings, progression, and chat logs
- legacy HTTP launcher/game/registration/inventory/creature/QoS APIs and static
  resources
- QoS UDP v1/v2
- RakNet payload bitstreams, offline negotiation, reliability datagrams,
  ACK/NACKs, game state, and action commands
- gameplay objects, attributes, collision, locomotion, players, parties,
  catalysts, levels, markersets, noun data, combat, and events
- pure-Go Lua runtime for the external global and ability scripts
- server-data extraction/merge support in the `installer` package

## Mutation Agent horde settings

Gameplay policy settings live in `server.toml` beside the selected `darkspin.toml`. The server adds the following section when it is absent. These are adjustable compatibility rules; the original server's horde selection policy has not been recovered.

```toml
[mutation_agent]
version = 1
is_enabled = true
minimum_chain_level = 7
wave_count = 3
spawn_wave = 3
inter_wave_delay_millisecond = 1500
is_agent_defeat_completing = true
```

- `minimum_chain_level` is the one-based campaign index, from 1 to 72; 7 is mission 2-3.
- `wave_count` accepts 1–10. `spawn_wave` accepts 1 through `wave_count` and adds one Mutation Agent alongside that wave's normal enemies.
- `inter_wave_delay_millisecond` accepts 0–30000. A disconnect or retry does not shorten the retained deadline.
- `is_agent_defeat_completing = true` ends the encounter when its Mutation Agent dies, even if other wave members remain. Set it to `false` to require normal wave clearing.
- Disabling the section's behavior, or playing below its minimum index, retains the earlier ordinary/tutorial horde rules. It does not disable a developer-spawned Mutation Agent's authored abilities.

Use the launcher's **Server Rules → Mutation Agent Hordes** section to edit these settings, then select **Apply Server Rules**. Reload restores the saved settings, and Share to GitHub includes the Mutation Agent section. Changes apply to new missions; existing missions retain their captured policy. Stop the server before editing this file directly, then restart it. Invalid policy ranges reject configuration loading or saving. Authored beam timing, elite bonuses, aura damage, and transformation behavior are not overridden by these settings.

## Verify

```sh
go test ./...
go test -race ./...
go vet ./...
```

Repository convention: error-returning calls are assigned on their own line;
inline `if err := ...` conditionals are not used.
