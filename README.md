# Captain

Unified management panel for [bosun](https://gitlab.com/boyang-hu/bosun)
nodes: users, plans, orders and payments, subscription output, node fleet,
forwarding policy and configuration push. One Go binary with the admin console
and user portal embedded. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Status

Backend skeleton, verified by an end-to-end API test and a live run with a
real bosun (Captain driver) serving mieru to the official client, with the
traffic landing in the user's subscription:

- SQLite database with embedded migrations (users, sessions, plans,
  subscriptions, orders, nodes, inbounds, entries, chains, traffic, online
  devices, forward status, settings).
- Admin API: login with argon2id + session cookie, create nodes (one-time
  pairing code), inbounds, user groups, users, plans, grant a plan, user detail.
- Per-inbound access: an inbound bound to a user group only provisions that
  group's users (`spec.Inbound.ScopedUsers`); ungrouped inbounds get every
  user with a usable subscription.
- Agent API implementing `bosun/pkg/agentproto`: pair, state with ETag,
  report (traffic charged to the active subscription, online devices,
  forward status, node liveness). A user whose subscription expires or runs
  out of quota disappears from the node's desired state.

Not yet: bosun's Captain driver, subscription renderers, orders and payments
(EPay, Stripe), user portal API, React frontends, jobs.

## Run

```sh
make build
cp config.example.yaml /etc/captain/config.yaml       # set base_url
bin/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
bin/captain serve -c /etc/captain/config.yaml
```
