# awake

`awake` is a local macOS CLI/TUI that supervises `caffeinate -is`, confirms the corresponding sleep assertion, and displays the health signals useful when working remotely through Herdr, Tailscale, and SSH.

It never calls `sudo`, changes `pmset` settings, opens remote connections, sends telemetry, or stores credentials.

## Install

```sh
go install github.com/teasec4/awake/cmd/awake@latest
```

Or from a checkout:

```sh
go install ./cmd/awake
# or build a local binary
go build -o awake ./cmd/awake
```

`go install` places the binary at `$(go env GOPATH)/bin/awake`. Make sure that directory is on your `PATH`.

`caffeinate` and `pmset` are supplied by macOS. Check the machine first:

```sh
awake doctor
```

## Use

```sh
awake start              # run in the background, returns immediately
awake run                # foreground alternative with a live TUI
awake status
awake status --json
awake stats --watch --interval 2s
awake stop
```

`awake start` (alias `awake --start`) launches awake in the background as a detached supervisor and returns immediately; the session stays awake until `awake stop`. Its output is written to `~/Library/Application Support/awake/logs/awake.start.{out,err}.log`. `awake run` is the foreground alternative: it owns only the `caffeinate -is` process it starts, has a compact live TUI, and `q`, Escape, or Ctrl+C stop it cleanly. `awake run --no-tui` is intended for launchd and reports concise output. `awake status --json` is suitable for health checks and agents.

`awake stop` stops the background supervisor and the `caffeinate` it owns; with no supervisor it stops only the awake-owned `caffeinate`. `awake` considers its own state active only when the saved PID is still `caffeinate` and `pmset -g assertions` identifies an assertion for that exact PID. A manually started `caffeinate` is reported as external and is never stopped by `awake stop`.

## Herdr, Tailscale, and SSH

Run `awake start` in its own Herdr pane, then detach (or use `awake install` for always-on supervision):

```text
Mac is on and connected to power
        |
awake start
        |
Herdr detach
        |
Codex/Kimi continue working
        |
Phone connects through Tailscale + SSH
        |
herdr
```

The tool only checks local Tailscale status (when the binary is installed) and whether TCP port 22 is already listening. It never initiates network traffic.

## LaunchAgent

```sh
awake install --dry-run
awake install
awake uninstall
```

Installation writes only `~/Library/LaunchAgents/dev.awake.plist`, uses the absolute path to the current binary, and logs to `~/Library/Application Support/awake/logs/`. It uses the current user's `launchctl bootstrap gui/<uid>` flow and is idempotent. A LaunchAgent starts after user login; after a reboot, FileVault must be unlocked before this can happen.

## State and diagnostics

State is stored in `~/Library/Application Support/awake/` as `awake.pid`, `awake.json`, and `logs/`. The supervisor has an exclusive lock and removes safely detected stale state. Use `awake doctor` for command availability, architecture, power, assertion, network, state, and LaunchAgent checks.

## MacBook limitations

- Closing the lid can prevent normal remote operation despite a user-space assertion; keep the Mac connected to power and use an appropriate clamshell setup.
- `caffeinate -is` prevents idle/system sleep while its process remains alive, but it does not override a depleted battery or forced shutdown.
- FileVault requires local unlock after restart before a user LaunchAgent can run.
- Apple Silicon temperature is intentionally reported as `unavailable`: reliable readings require privileged APIs on many macOS versions, and `awake` will not use `sudo` or `powermetrics`.

## Development

```sh
make build        # builds ./awake with version info baked in
make test         # go test ./...
make vet          # go vet ./...
gofmt -w .
./awake doctor
./awake status --json
./awake start
./awake run
./awake stop
```

Releases are built with [goreleaser](https://goreleaser.com) (`.goreleaser.yml`): `make release` builds darwin amd64/arm64 binaries, attaches them to the GitHub release for the current tag, and writes a `checksums.txt`. Requires a `GITHUB_TOKEN` and a `vX.Y.Z` tag.
