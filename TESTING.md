# Testing

Most of Meeseeks is tested with plain `go test`:

```bash
make test        # go test ./...
```

Tests run only for the current platform: files are guarded with build tags
(`//go:build darwin`, `//go:build linux`, `//go:build windows`), so on macOS
you exercise the darwin code, on Linux the linux code, and so on.

The `start-at-login` feature is the one area that is platform-specific — each
OS uses a different native mechanism (see the platform table in the
[README](README.md)). This document explains how
to test the Linux and Windows implementations, including from a macOS
development machine.

## What the login tests cover

The unit tests focus on the parts that are safe to run anywhere — they do
**not** register real services with `launchd`, `systemd`, or Task Scheduler.
The test directory is redirected with the `MEESEEKS_TEST_LOGIN_DIR`
environment variable so nothing touches the real system:

| Test | What it verifies |
| --- | --- |
| `*_Create_ServiceAlreadyExists` | A second `Create` fails when a definition already exists |
| `*_Create_Renders*` | The generated service definition contains the right fields (restart policy, trigger, exec/config paths) |
| `TestLinuxService_Enable_RequiresSystemd` | `Enable` returns a clear error when `systemctl` is not on `PATH` |
| `TestWindowsService_Create_RendersTaskXML` | The task XML is UTF-16LE with a BOM (required by `schtasks /xml`) |

`Enable`/`Disable` are intentionally **not** unit-tested against the live
scheduler, because they mutate real per-user system state.

## Linux

### Run the Linux tests natively

On a Linux host:

```bash
go test ./internal/login/ -v
```

### Run the Linux tests from macOS (or any non-Linux host)

The Linux login tests are pure Go (file generation + a `PATH` check), so they
run in a stock Go container. Use the Go version from `go.mod`:

```bash
docker run --rm -v "$PWD":/src -w /src golang:1.26 \
  go test ./internal/login/ -v
```

You should see the three `TestLinuxService_*` tests pass. The container has no
`systemd`, which is exactly why `TestLinuxService_Enable_RequiresSystemd`
exercises the "systemd not available" path.

### Manual end-to-end check on Linux

Requires a host with a running `systemd --user` session:

```bash
make build
./meeseeks start-at-login enable
systemctl --user status meeseeks.service   # should be active (running)
./meeseeks start-at-login status
./meeseeks start-at-login disable
systemctl --user status meeseeks.service   # should be gone
```

The unit file is written to `~/.config/systemd/user/meeseeks.service`.

## Windows

There is no cross-platform way to run Windows binaries from macOS/Linux, so the
Windows verification is split into two parts.

### Cross-compile check (from any host)

This proves the Windows code and its tests compile:

```bash
GOOS=windows go build ./...                    # whole binary builds for Windows
GOOS=windows go test -c -o /dev/null ./internal/login/   # test binary compiles
```

### Run the Windows tests natively

On a Windows host (PowerShell or cmd) with Go installed:

```powershell
go test ./internal/login/ -v
```

You should see the `TestWindowsService_*` tests pass.

### Manual end-to-end check on Windows

Requires Windows with Task Scheduler (built in):

```powershell
go build -o meeseeks.exe ./cmd/meeseeks
.\meeseeks.exe start-at-login enable
schtasks /query /tn meeseeks               # task should exist
.\meeseeks.exe start-at-login status
.\meeseeks.exe start-at-login disable
schtasks /query /tn meeseeks               # should report "cannot find the task"
```

The task XML is written to `%LOCALAPPDATA%\meeseeks\meeseeks-task.xml`.

## Cross-platform build sanity check

To confirm every release target still compiles after a change:

```bash
go build ./...                 # host platform
GOOS=linux   go build ./...
GOOS=windows go build ./...
```

Or, with [GoReleaser](https://goreleaser.com) installed, build all release
targets at once without publishing:

```bash
goreleaser build --snapshot --clean
```
