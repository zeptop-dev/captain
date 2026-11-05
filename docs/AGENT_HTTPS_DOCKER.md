# Agent HTTPS with Docker and 1Panel

Captain can serve its Agent API through an additional HTTPS listener using a
certificate from its certificate inventory. Configure and enable that listener
in the administration console. Publishing a Docker port alone does not enable
it, issue a certificate, or prove that a remote node can connect.

## New script installation

Add `--agent-https-port 2096` to the usual Docker installation command:

```sh
sh install.sh --mode docker --behind-proxy --domain panel.example.com \
  --admin-email admin@example.com --agent-https-port 2096
```

The installer still asks for the admin password. In bridge mode it publishes
`2096:2096/tcp`, alongside the existing website mapping. It preserves the usual
1Panel/OpenResty network attachment. Host mode needs no port mapping. The flag
only changes deployment networking: it writes no new `config.yaml` keys, so
using the script with an older Captain version does not break strict config
parsing. The listener itself requires a Captain version that supports it.

Choose the same port in Captain and bind its listener to `:2096` (all container
interfaces), rather than `127.0.0.1:2096`. Configure the hostname and choose an
existing certificate that covers it. Website HTTPS can continue through 1Panel
on 443. Agent traffic on 2096 reaches Captain directly and needs no extra
1Panel reverse proxy.

## Existing script installation

Use the dedicated operation; do not reinstall or use `--reconfigure`:

```sh
sh install.sh enable-agent-https --agent-https-port 2096 \
  --agent-https-url https://panel.example.com:2096 --agent-firewall auto
```

This requires Python 3 and a current Docker Compose plugin. It identifies the
running Captain service in `/opt/captain` (or an explicit trusted `--dir`), validates root-owned deployment
inputs, and creates `compose.agent-https.yaml`. Existing Compose input order,
custom networks, volumes, environment entries and the updater image override
are retained. The container is recreated without pulling/building an image or
initializing accounts. Its image must match the running image. Brief management
connection interruption during recreation is expected. A deployment lock covers
validation, recreation, recovery and updater registration, so simultaneous
installer invocations cannot undo each other’s changes.

The operation refuses ambiguous/custom deployment layouts, changed Compose
configuration, a different locally resolved image, occupied ports, and an
existing override not owned by this installer. Such deployments should use the
manual method below. Failed application restores the prior deployment files and
attempts to restore the previous service. Success and recovery both inspect the
running container; recovery also checks the original image, ports and Compose
identity. If the image defines a Docker healthcheck, it must become healthy within
180 seconds before the operation reports success or recovery. This confirms the
existing Docker healthcheck only, not public TLS or Agent reachability. Recovery
failure is reported explicitly. Forced process termination or a host power loss
is not covered by automatic recovery; inspect the deployment before retrying.

### Installations with the Docker web updater

The updater and Compose recreation must not run simultaneously. This operation
never stops a running updater. It tries the updater's own exclusive worker lock
and refuses to proceed while the updater is running. Arrange a maintenance
window in which no administrator initiates an upgrade, let any current upgrade
finish, and then stop `captain-updater` using the host's service manager. Run the
same command above during that window.

Stopping the updater is not an atomic pause of upgrade admission: a concurrent
upgrade request can race with a manual stop. The installer additionally checks
the persistent job while holding the worker lock and refuses any unfinished or
failed job. Recover that job through the existing updater workflow before
retrying; do not delete its state to bypass the check.

Ordinary quoted `COMPOSE_FILE` declarations are supported, and other `.env`
values—including multiline quoted values—are preserved without rewriting their
bytes. The currently released updater has a separate line-based `.env` writer.
If a multiline value contains a line that writer would mistake for a
`COMPOSE_FILE` declaration, this operation refuses the updater-managed deployment
before changing files. Use manual maintenance for that case.

After successful recreation, the operation re-registers the updater with the
complete Compose file list and restarts it. This preserves the Agent port on
future image upgrades. If registration fails, the command reports that the port
was applied but the updater needs repair; leave it stopped and run
`sh install.sh enable-web-upgrade` before future upgrades. A rejected or failed
operation does not automatically restart an updater that you stopped yourself.

## 1Panel-managed or custom Compose deployment

Edit the existing **Captain** service in the Compose project that actually owns
it, retaining all current ports, networks, mounts and image settings:

```yaml
services:
  captain:
    # Keep the rest of your existing service configuration.
    ports:
      # Keep existing port entries here as well.
      - "2096:2096/tcp"
```

Apply that Compose change through 1Panel or your usual deployment workflow.
For `network_mode: host`, do not add `ports`; the listener already binds the host.
If the Captain web updater also owns this deployment, coordinate the change in
a maintenance window and re-register it after changing the Compose file list.

Recreate the service after changing its ports; merely restarting an existing
container does not apply new publications. Use the actual project directory
from 1Panel, not a second copy of its Compose files.

Open the chosen TCP port in the host and provider firewalls if needed. For a
Cloudflare-proxied hostname, choose an HTTPS port supported by Cloudflare and
use a matching origin listener and valid certificate; no DNS port mapping is
involved. Whether a particular node provider allows that destination port must
be checked from the node itself. Listener readiness, Docker publication, public
TLS validation and node reachability are separate checks.

Changing the deployment port or the console's install settings does not migrate
existing bosun connections. Install/reconfigure nodes through their documented
workflow; this operation does not modify node credentials or accounting journals.

## Firewall setup and layered checks (unreleased)

`enable-agent-https` defaults to `--agent-firewall auto`. The same firewall step
runs for a new Docker install with `--agent-https-port` and for Docker host
networking. It only acts when exactly one active UFW or firewalld manager can be
identified. It never enables, resets or reloads a firewall. Existing allowed
rules remain operator-owned. Conflicting UFW deny/reject rules stop automatic
changes. New rules carry a recorded identity, and a failed firewall update only
rolls back that invocation's rules; it never deletes unrelated rules or old ports.
Firewalld requires a single identifiable active zone and checks runtime and
permanent configuration separately. `--agent-firewall skip` leaves host rules
alone and reports the skip. Other managers and ambiguous zones require manual
configuration. Provider security groups and Docker forwarding filters remain
outside this helper.

After the mapping succeeds, a failed firewall or network check retains the
working deployment and reports the failing layer. Do not retry installation
with `--reconfigure`. For an existing deployment, use this read-only check:

```sh
sudo sh install.sh check-agent-https --agent-https-port 2096 \
  --agent-https-url https://panel.example.com:2096 --dir /opt/captain
```

It inspects actual Docker publication and host firewall rules, then checks
loopback TLS with the configured hostname and a request to the domain from the
host. Both HTTPS checks use system certificate verification, a bounded response
and fixed Agent health path without redirects. The health response identifies
an Agent service; use the console diagnostic to confirm it is this Captain
process. Exit code 0 means the requested known layers passed, 2 means one needs
attention, and 1 means the deployment or parameters could not be safely handled.
Unknown cloud filters remain unknown even when the checks pass. Omitting the URL
on `enable-agent-https` skips the network checks; it does not imply reachability.

The console's **Check entry** button separately tests the saved origin from the
Captain process and verifies a fresh challenge response. It cannot inspect the
host's firewall or edit Compose files. Run the displayed node command on the
target node to test that node's own path. DNS, TCP, TLS, redirects, unexpected
services and HTTP errors (including 522) are reported separately. This does not
bypass provider network policy. Native binary installations still configure
host firewall rules through their normal host administration tools.
