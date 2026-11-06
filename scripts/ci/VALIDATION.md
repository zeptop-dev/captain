# Exact-commit release CI reuse

A normal master push still runs the complete race and coverage suite. A tag
release can reuse that result instead of running the same Go suite a second
time. All frontend checks/builds, formatting, vet, the current vulnerability
scan, and release builds still run. There is no package selection, timeout
reduction or assertion removal.

`reuse.py` accepts only the latest run of the repository's active `ci.yml`
workflow, for a `push` to the default `master` branch, with the tag's exact
checked-out SHA. Both repository identities must match, and GitHub's workflow
source at that SHA must equal the checked-out validation configuration. Every
job and step must be completed and successful, including all named required
checks. Jobs are read from the latest run attempt, then the run and run list
are checked again. A failed rerun, newer incomplete run, missing/skipped check,
workflow restructuring, API error or pagination ambiguity all retain the
original full release suite. Renaming/splitting CI therefore requires updating
and reviewing this explicit contract. GitHub's authenticated Actions metadata
is the trust boundary; this is not independent attestation of the runner.

The lookup runs in a separate job with read-only Actions and contents access.
It does not download artifacts or execute output from another workflow. The
release rebuilds both Linux architectures from source and checks static
linkage, the pinned Go toolchain, exact VCS revision, clean build provenance,
and SHA256 files. The native amd64 artifact also runs version, migration,
HTTP health and all four embedded SPA asset smoke checks against an isolated temporary
database. Arm64 remains cross-built/static/toolchain/VCS-checked; the image build also cross-compiles it, so neither check claims arm64 runtime
execution.

Both workflows support `workflow_dispatch` for development branches. A manual
release run **always runs the full fallback** and publishes neither releases
nor images. Manual CI, pull-request CI, and another repository's runs never
qualify as trusted master-push evidence. When editing these workflows, run an
actual non-publishing release dispatch before a product tag. Manual image builds use the fixed local `dry-run` tag/version, so normal
`task/*` branch names work without creating a special rehearsal branch. Product
tag pushes retain their version and `latest` tags.

Local helper regression checks:

```sh
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/ci -p 'test_*.py'
```

The API tests cover identities, exact commit/configuration, event type, failed
or skipped checks, reruns/newer runs, pagination, malformed responses and
network failures. A missing trusted run is an optimization miss, never a way
to publish without validation. Reuse saves the duplicate suite's duration;
the first full CI and the mandatory manual rehearsal still need to finish.
