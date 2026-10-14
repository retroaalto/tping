# Agent Guide

## Project

Small Go CLI: a live terminal ping latency graph built with Bubble Tea. Single `main` package. Linux only: macOS and Windows are not supported, and `trace.go` has a `linux` build tag.

## Build

```sh
./build.sh        # produces bin/tping
go build .        # also works
```

`bin/tping` is gitignored.

## Run

```sh
go run . 1.1.1.1
go run . -i 0.5 1.1.1.1
go run . -max-hops 10 -trace-every 2 -n 1.1.1.1
```

Press `q`, `Esc`, or `Ctrl-C` to quit.

### Command-line flags

- `-i SECONDS`: Wait SECONDS between pings (default: 1)
- `-trace-every N`: Run traceroute sweep every N ping cycles; 0 disables tracing (default: 1)
- `-max-hops N`: Maximum number of hops in traceroute (default: 30)
- `-n`: Disable reverse DNS lookup, show IP addresses only

## Test

```sh
go test ./...
go test -run TestApplyTrace ./...   # single test (name prefix match)
go vet ./...
```

Tests cover model/view logic only; they do not send real pings.

## ICMP permissions

On Linux, unprivileged ICMP may require system ping permissions to be enabled (e.g. `net.ipv4.ping_group_range` or `setcap cap_net_raw+ep` on the binary). The tool never requests elevated privileges itself.

## Publishing releases

Releases are created when a `v*` tag is pushed. GoReleaser uploads the Linux archives to GitHub and updates the `Formula/tping.rb` file in the `retroaalto/homebrew-tping` tap.

Before publishing the first release, create a public `retroaalto/homebrew-tping` repository with a `main` branch, then add a `HOMEBREW_TAP_GITHUB_TOKEN` Actions secret to this repository. The token must have repository contents write access to `retroaalto/homebrew-tping`.

## Implementation notes

- Bubble Tea model is in `model.go`; `main.go` wires flags and starts the program.
- Pings use `github.com/go-ping/ping` with `SetPrivileged(false)`, `Count=1`, and a 900 ms timeout.
- Traceroute uses Linux ICMP datagram sockets with IP_RECVERR to capture ICMP TTL-expired messages (`trace.go`).
- `main.go` validates exactly one positional host argument and positive numeric flags.

## Architecture

Two independent measurement streams feed one `model` (`model.go`):

- **Ping stream:** `tickMsg` → `runPing` → `pingMsg` → `recordPing`. Drives the graph and the footer stats. After each `pingMsg` the next tick is scheduled, and every `traceEvery`-th ping starts a trace if none is in flight.
- **Trace stream:** `runTrace` → `traceHost` (`trace.go`) → `traceMsg` → `applyTrace`. One sweep probes TTL 1..`maxHops` sequentially, each probe with its own ICMP datagram socket. It stops at the first echo reply, any ICMP destination-unreachable, or a `fatal` local error. `applyTrace` merges results into `m.hops` by index. A hop's stats reset only when a different non-nil IP answers at that TTL. Reverse DNS runs as separate `traceHostMsg` commands.

The footer stats and the last hop row come from different packets, so they intentionally don't match exactly.

## Gotchas

- `trace.go` has `//go:build linux`, but `main.go` and `model.go` use its types (`hop`, `traceResult`, `traceConfig`), so non-Linux builds fail. That is accepted; do not add stubs.
- `traceResult.fatal` (socket, send, or receive failures) aborts the sweep and surfaces as `m.traceErr`; the view shows that error instead of the hop table. Plain `errTimeout` and "unrecognized ICMP response" do not abort.
- `traceRowsAvailable` hard-codes the number of non-hop rows in `View`; change both together. Table rows are clipped to terminal width with `lipgloss.MaxWidth`.
- `README.md` is for human users only; keep agent and maintainer notes in this file.
- The Homebrew formula test (`.goreleaser.yaml`) runs `tping -h 2>&1` because Go prints usage to stderr.
- `.opencode/` is gitignored tooling, not part of the project.
