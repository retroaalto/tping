# tping

Live terminal ping graph with a built-in traceroute. Each reply adds a latency bar to the timeline, failed pings are shown as `×`, and a hop table below shows every router on the path to the target.

Linux only: traceroute uses Linux ICMP socket APIs.

## Install

The recommended way to install tping is Homebrew (Linux `amd64` and `arm64`). Releases are published to the [`retroaalto/homebrew-tping`](https://github.com/retroaalto/homebrew-tping) tap.

```sh
brew install retroaalto/tping/tping
```

This taps the repository and installs the formula in one step. You can also do it in two steps:

```sh
brew tap retroaalto/tping
brew install tping
```

Update with `brew upgrade tping` and remove with `brew uninstall tping`. If you get a permission error when running it, see [Permissions](#permissions).

To build from source instead, you need Go:

```sh
./build.sh    # produces bin/tping
```

## Usage

```sh
tping [-i seconds] [-trace-every N] [-max-hops N] [-n] <host-or-ip>
```

```sh
tping 1.1.1.1
tping -i 0.5 example.com
tping -n -max-hops 10 1.1.1.1
```

| Flag | Meaning | Default |
| --- | --- | --- |
| `-i SECONDS` | Wait between pings | 1 |
| `-trace-every N` | Run a traceroute sweep every N pings; 0 turns tracing off | 1 |
| `-max-hops N` | Maximum number of hops, 1 to 255 | 30 |
| `-n` | Skip reverse DNS and show IP addresses only | off |

Press `q`, `Esc`, or `Ctrl-C` to quit.

## Permissions

tping sends unprivileged ICMP and never asks for elevated privileges. If you get a permission error, allow ping sockets for your group:

```sh
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

or give the binary the raw-socket capability:

```sh
sudo setcap cap_net_raw+ep "$(which tping)"
```

## Reading the screen

The graph shows one bar per ping, newest on the right. In the footer, LATEST, MIN, AVG and MAX come from the pings, and SENT and LOSS count pings sent and lost. The hop table is described below.

### Hop table

Each row is one router on the path to the target, refreshed every traceroute sweep.

- LOSS: share of probes to that hop that got no reply.
- LATENCY: bar for the hop's *most recent* round-trip time. Because it shows only the latest sample, it can differ from AVG and change between sweeps.
  - Length is relative: the hop with the slowest latest sample fills the column, and the others are scaled against it (the scale never drops below 10 ms).
  - Color uses absolute thresholds: green < 50 ms, yellow < 150 ms, red ≥ 150 ms.
  - `×` means the hop has not replied yet.
- MIN / AVG / MAX: statistics over all replies from that hop.

A long bar at one hop followed by shorter bars after it usually means that router is slow to answer ICMP, not that it slows your traffic. Real added latency shows up as long bars that stay long from that hop to the end of the path.

## How traceroute works

Every packet has a TTL that each router lowers by one. When it hits zero, the router drops the packet and replies "time exceeded" from its own address. tping sends probes with TTL 1, 2, 3, … so each router on the path reveals itself, until the target answers.

## Why the bottom stats differ from the last hop

The footer comes from the regular pings; the last hop row comes from traceroute probes. They are separate packets with different sample counts, so MIN/AVG/MAX and loss won't match exactly.
