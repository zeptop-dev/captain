# Host upgrades and complete removal

The installer now accepts explicit `install`, `upgrade`, `enable-web-upgrade`
and `uninstall` actions. Run on the Linux host as root. These commands are for
installer-managed paths: `/opt/captain`, `/etc/captain` and
`/var/lib/captain`. System packages, SSH, Docker itself, external reverse
proxies and unrelated applications are never removed.

```sh
# Upgrade an existing binary or Docker Compose installation, preserving settings.
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sudo sh -s -- upgrade
# Optionally select a newer stable release with --version vX.Y.Z.

# Enable Docker web updates once (also supported by install --web-upgrade).
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sudo sh -s -- enable-web-upgrade

# Permanently remove the application and its local data.
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sudo sh -s -- uninstall --yes
# Add --keep-data to retain application configuration, data volumes and backups.
```

Non-interactive uninstall requires `--yes`; an interactive terminal instead asks
for `yes`. Default removal includes service units and drop-ins, executables and
old executable backups, application containers, unused application data volumes,
unused project networks, removable official images, configuration, database,
certificates, local backups, application log files and the optional updater.
Shared resources are retained: a volume still used by another container stops
removal with an error, and images in use are never forcibly deleted. Docker-run
containers using the official image are also discovered. Custom Compose working
directories, outside data mounts and custom data paths require explicit operator
handling; the installer must not guess which other directories it may delete.
System journald history is shared with other services and is not vacuumed.
Remote/object-storage backups and files copied elsewhere by the operator are not
part of local uninstall.

## Docker web upgrades

Settings → Backups and maintenance → Version and updates uses the existing update
API. After host registration, the Docker update button starts a persisted job:
check deployment/release, pull image, stop the application, copy its complete data
and configuration, recreate only that service, and verify the running process's
version and readiness. The page displays the phase and survives reconnection.
The application keeps running while the image downloads. A successful HTTP
response from the old application is not treated as upgrade completion.

The optional `captain-updater` host service supports systemd and OpenRC. It runs
a private copy of the executable from `/usr/local/lib/captain-updater/worker`.
Only the dedicated Unix socket under `/run/captain-updater` is mounted into the
application; Docker's daemon socket is never given to the web process. The helper
is still a privileged component. Its API accepts a stable version only, uses
fixed official repositories, and cannot execute arbitrary commands, select paths,
register deployments or uninstall anything. Registration is a host-root operation.
Rootless Docker and custom container UID mappings are not supported.

Registration records the Compose project/files and adds `compose.updater.yaml`
and `COMPOSE_FILE` in `.env`. Existing ports, volumes, environment and networks
are retained. Deployment inputs and their parent directories must be root-owned
and not writable by the application; Compose `include`/`extends` and custom
application startup commands are rejected. Data must live in the standard data
volume so that backup really covers it. A bare `docker run` installation must
first be recreated as Compose with the same data volume to use web updates.

The installed image is pinned locally; use the installer `upgrade` action or the
web button for future upgrades. A plain `docker compose up -d` uses the same pinned
image and cannot silently restore an older version from the original YAML.
For installations predating this feature, run the **upgrade** action once; merely
registering an old image does not add the new button to its old frontend.

```sh
sudo /usr/local/lib/captain-updater/worker docker-updater status
sudo /usr/local/lib/captain-updater/worker docker-updater upgrade --version vX.Y.Z
sudo /usr/local/lib/captain-updater/worker docker-updater retry
```

`retry` permits failed checks/downloads/backups. After container recreation has
begun, inspect the actual container and saved backup first; migrations might have
run even when Compose returned an error. Interrupted jobs are marked failed after
helper restart, never replayed automatically. A managed node's repeated upgrade
request cannot repeatedly run a failed job.

## Backups and failure handling

Docker backups are stored outside the application at
`/var/lib/captain-updater/backups/<job-id>/`: data, application configuration,
and numbered copies of the Compose inputs, override and `.env`. The directory is
root-only. The application is stopped while data is copied, including SQLite WAL
files. A backup/download failure does not launch the new image; after a backup
failure the old container is restarted. Native `upgrade` likewise stages and
checksums the new executable first, stops the service for a full data/configuration
archive, preserves the previous executable and service configuration, and checks
that the new process starts.

There is no automatic rollback across database migrations. Once the new process
may have run, a failure leaves the image/data and backup available for diagnosis.
To restore, stop the application, restore matching pre-upgrade data/configuration
and the corresponding image/binary together, then start it. Later writes will be
lost. Keep backups until verification is complete, then remove older job backups
on the host as appropriate; they are not automatically pruned.

Readiness checks the actual application process and local listener, including the
expected version. Its private socket lives in the application data directory,
so systemd read-only/private temporary directories do not break startup. For a
custom data directory, pass `healthcheck --data-dir /path/to/data`. It does not
prove public DNS/TLS reachability, remote upstream
availability or every proxy protocol. Both architectures use published images;
no source build is performed on the host.
