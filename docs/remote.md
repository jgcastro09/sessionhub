# Remote mode

Remote mode connects two running Session Hub instances over Tailscale. It does
not provide a web page, cloud relay, public listener, daemon, or separate
server.

The Host settings require selecting one local Tailscale IPv4 (`100.64.0.0/10`)
or Tailscale ULA IPv6 address. Startup fails if the address is not assigned
locally or is outside those ranges.

Remote clients can inspect sessions, Executors, terminal snapshots, queues,
pipelines, metrics, and logs; create checkpoints; request or release terminal
control; send input and resize while they own control; and approve or cancel
steps. All lease transitions and mutating requests are persisted.

Only one actor controls a terminal. Other clients observe and may request
control. On disconnect, unacknowledged input is discarded and is never
automatically resent. The Executor can continue on the Host until the Host TUI
closes.
