# Darkrun with Docker Compose

This setup runs password-based account login and gameplay together in one headless server. A small container starts `darkrun bootstrap`, which supervises a second Darkrun process and updates its mounted binary.

**Stable setup is not available yet. Use unstable.** You do not need to clone the repository, install Go, or build an application image.

## Download and prepare

You need Linux amd64, Docker Engine with the Compose plugin, a reachable IPv4 address, and a complete build-103 game installation.

[Download the unstable Compose setup ZIP](https://nightly.link/darkspinnet/darkspin/workflows/main-build/main/darkrun-compose-unstable.zip). Extract it into its own directory, such as `/srv/darkspin`. It contains:

```text
compose.yaml
.env.example
README.md
VERSION
```

This same guide is included in the ZIP. `VERSION` identifies the setup bundle; Darkrun binaries update independently.

The download and initial binary feed become available after the updated `main-build` workflow succeeds. The `darkrun-bootstrap:1` GHCR infrastructure image must also be published and allow public pulls.

Run subsequent commands from that directory:

```sh
cd /srv/darkspin
cp .env.example .env
mkdir -p darkspin
sudo chown -R 65532:65532 darkspin
```

Edit `.env`:

```dotenv
COMPOSE_PROJECT_NAME=darkspin
DARKSPIN_ADVERTISED_HOST=192.0.2.10
DARKSPIN_PORT=42127
DARKSPIN_HOME_PATH=./darkspin
DARKSPIN_GAME_HOST_PATH=/srv/game-build-103
```

Replace the example address with the IPv4 players can reach and the game path with your actual installation. The installation root must contain `DarksporeBin/` and `Data/`. UID/GID 65532 must be able to read and traverse it. Allow the configured port for **both TCP and UDP**, using the same port inside and outside the container.

Compose mounts the game **read-only at /game** and sets `DARKSPIN_GAME_PATH=/game`. Keep it mounted permanently: first startup and later updates rebuild missing or outdated content from it. Darkrun validates the source build and required package indexes even when content is current. Do not supply or link an external `content.db`. No game assets are included in the ZIP, image, or binary.

## Start and stop

```sh
docker compose pull darkrun
docker compose up -d --no-build darkrun
docker compose logs -f darkrun
docker compose ps
```

On first start, the entry point downloads a versioned manifest and its exact binary artifact, verifies the executable's SHA-256 and build identity, then runs `darkrun bootstrap`. Later container starts use the installed binary and bootstrap's recovery state. The server prepares content and generates saved settings and its authentication secret before accepting players.

Connect through Darkspinner's Remote Servers account flow using the advertised address and port. This prototype uses username/password login; HTTPS and OAuth are not configured.

```sh
docker compose stop darkrun
```

An intentional container stop shuts down the child and exits bootstrap. Container health checks verify the maintenance endpoint and main server endpoint; they do not prove a multiplayer session works.

## Persistent layout

The entire `darkspin/` host directory is mounted writable at `/darkspin`:

```text
darkspin/
  bin/darkrun
  runtime/
    darkspin.toml
    server.toml
    darkspin/
      saves/
      cache/
      logs/
  backup/
    YYYY-MM-DD/
      <timestamp-and-build>/
  update-request.json   (only while maintenance is pending)
  firstboot.json        (only during an update transaction)
```

Bootstrap fixes configuration and generated content inside this runtime so every rollback covers the same data. Do not mount an individual executable: updates replace it atomically. Do not point multiple servers at the same directory or SQLite saves.

To migrate an earlier setup, stop its server, copy its complete runtime (including `darkspin.toml`, `server.toml` if present, and the `darkspin/` subtree) into the new `darkspin/runtime/`, and make the new mount writable by UID/GID 65532. Preserve the original separately until the new server works. Remove any old updater service using its old Compose definition before switching. The new setup has no updater sidecar or Docker socket mount.

## Schedule and warnings

Scheduled maintenance is enabled by default, using **03:30 UTC daily** and a **30-minute warning**. Set optional overrides in `.env` or directly in Compose's environment:

```dotenv
DARKSPIN_IS_UPDATE_ENABLED=true
DARKSPIN_UPDATE_CRON="30 3 * * *"
DARKSPIN_UPDATE_WARNING_MINUTES=30
DARKSPIN_UPDATE_START_TIMEOUT_SECONDS=1800
DARKSPIN_BACKUP_RETENTION_DAYS=7
```

These inputs are saved under `[bootstrap]` in `runtime/darkspin.toml`. Empty or omitted overrides preserve saved settings, which are also editable through Darkspinner's Config interface. Explicit environment inputs take precedence on each container start. Recreate the service after changing its environment:

```sh
docker compose up -d --no-build darkrun
```

The cron expression uses five numeric fields in UTC, with lists, ranges and steps:

| Cadence | Expression |
| --- | --- |
| Daily at 03:30 | `30 3 * * *` |
| Sundays at 03:30 | `30 3 * * 0` |
| Monday and Thursday at 03:30 | `30 3 * * 1,4` |
| First day of each month at 03:30 | `30 3 1 * *` |

The default sends system-chat warnings at **03:30, 03:40, 03:50, 03:55, 03:56, 03:57, 03:58 and 03:59 UTC**, then a final message at **04:00** before disconnecting players. Every reminder targets players online at that moment, including those who joined after an earlier warning. Other warning durations use the initial warning, remaining multiples of ten minutes, and the final five minutes. The deadline remains fixed; message delivery and graceful saving may extend actual downtime beyond it.

Cancel the current countdown:

```sh
docker compose exec darkrun /darkspin/bin/darkrun maintenance cancel --config /darkspin/runtime/darkspin.toml
```

Cancellation skips that scheduled occurrence. To disable future automatic maintenance, set `DARKSPIN_IS_UPDATE_ENABLED=false` and recreate the service. Bootstrap still monitors and restarts a failed child.

## Updates, backups and rollback

Each scheduled maintenance window restarts the server, **even if no new binary exists**:

1. The server announces maintenance, disconnects players, flushes state, closes storage, and writes a clean-shutdown request.
2. Bootstrap waits for the child to exit and acquires the stopped runtime's lock. It snapshots the installed binary, configuration and complete saves directory together under a dated backup.
3. It reads the latest unstable manifest. An unchanged binary or unavailable download restarts the current version. A new candidate must match the platform and SHA-256 before atomic installation.
4. Bootstrap records `firstboot.json` before installing the candidate and starts it. The candidate prepares content from the required game mount.
5. Three consecutive readiness probes accept the new version. A crash or startup timeout restores the matching binary, saved settings and databases, then starts the previous version. Failed candidate data and cache are retained inside that backup for diagnosis.
6. An interrupted update is recovered on the next bootstrap start. Completed backups older than the configured retention period are pruned; the active rollback snapshot is protected.

The default startup allowance is 30 minutes. Increase it for a slow content rebuild. The supervisor also restarts an established server after repeated health failures, with backoff for repeated crashes. Logs go to the container output and runtime logs.

Backups contain account data and authentication secrets; restrict access to the host directory. Generated content is rebuilt rather than included in every snapshot. Keep an independent backup of the stopped host directory for disk loss or operator recovery. Automatic rollback protects candidate startup; a fault discovered after a candidate is accepted still requires operator recovery. Graceful shutdown does not promise exact mid-combat resume.

The running bootstrap process keeps its loaded supervisor code while replacing and restarting the child. A later container restart loads the current binary's bootstrap implementation.

## Infrastructure and setup upgrades

Ordinary Darkrun updates download a binary; they do not rebuild or replace the container image. The small infrastructure image and Compose files are upgraded deliberately when deployment tooling changes. Download a new setup ZIP into a separate directory, review it, and preserve your `.env` and mounted `darkspin/` data.

The default manifest is the `darkrun-update-linux-amd64-unstable` artifact from `main-build`. `DARKSPIN_UPDATE_MANIFEST_URL` may override its HTTPS URL; the manifest's binary URL points to one immutable Actions artifact. Expired or unavailable artifacts leave the installed server intact.

For packaging with `mage darkrun:compose` and infrastructure image development, see [INSTALL.md](https://github.com/darkspinnet/darkspin/blob/main/INSTALL.md#packaging-and-source-development).
