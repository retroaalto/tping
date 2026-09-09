# tping

Live terminal ping graph written in Go with Bubble Tea. Each response adds a latency bar to the timeline; failed responses are shown as `×`.

## Install with Homebrew

On Linux, after the first tagged release is published:

```sh
brew install retroaalto/tping/tping
```

The Homebrew package supports Linux `amd64` and `arm64`. macOS is not supported because traceroute uses Linux ICMP socket APIs.

## Publishing releases

Releases are created when a `v*` tag is pushed. GoReleaser uploads the Linux archives to GitHub and updates the `Formula/tping.rb` file in the `retroaalto/homebrew-tping` tap.

Before publishing the first release, create a public `retroaalto/homebrew-tping` repository with a `main` branch, then add a `HOMEBREW_TAP_GITHUB_TOKEN` Actions secret to this repository. The token must have repository contents write access to `retroaalto/homebrew-tping`.

## Run

```sh
go run . 1.1.1.1
# Use the same -i interval switch as ping; the value is in seconds.
go run . -i 0.5 1.1.1.1
```

Press `q`, `Esc`, or `Ctrl-C` to quit.

On Linux, unprivileged ICMP may require the system's ping permissions to be enabled. The tool does not request elevated privileges.
