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
sh install.sh enable-agent-https --agent-https-port 2096
```

This requires Python 3 and a current Docker Compose plugin. It identifies the
running Captain service in `/opt/captain`, validates root-owned deployment
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

Open the chosen TCP port in the host and provider firewalls if needed. For a
Cloudflare-proxied hostname, choose an HTTPS port supported by Cloudflare and
use a matching origin listener and valid certificate; no DNS port mapping is
involved. Whether a particular node provider allows that destination port must
be checked from the node itself. Listener readiness, Docker publication, public
TLS validation and node reachability are separate checks.

Changing the deployment port or the console's install settings does not migrate
existing bosun connections. Install/reconfigure nodes through their documented
workflow; this operation does not modify node credentials or accounting journals.
