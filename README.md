# tping

Live terminal ping graph written in Go with Bubble Tea. Each response adds a latency bar to the timeline; failed responses are shown as `×`.

## Run

```sh
go run . 1.1.1.1
# Use the same -i interval switch as ping; the value is in seconds.
go run . -i 0.5 1.1.1.1
```

Press `q`, `Esc`, or `Ctrl-C` to quit.

On Linux, unprivileged ICMP may require the system's ping permissions to be enabled. The tool does not request elevated privileges.
