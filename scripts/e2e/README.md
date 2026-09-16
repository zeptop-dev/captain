# Live regression (`scripts/e2e/live.py`)

Unit tests cannot tell you that a core is missing a build tag, that a
client refuses an ALPN, or that a report never reaches the panel. This
script runs against a real Captain with real paired nodes and a real
mihomo client, in about a minute, and is meant to run before every
release:

1. every paired node is online and its last doctor report has no failed
   check and no "not applied" inbound;
2. the test user's subscription renders for mihomo and every proxy in it
   passes a delay test through a headless mihomo (real handshakes, all
   protocols);
3. a download through one proxy is charged to the user within the report
   window.

Run it from a machine that can reach the panel and the nodes:

```sh
export CAPTAIN_URL=https://panel.example.com
export CAPTAIN_API_TOKEN=cap_...        # Settings → API tokens (admin)
export E2E_USER_ID=2                    # a test user with an active plan
export MIHOMO_BIN=/path/to/mihomo       # e.g. "/Applications/Clash Verge.app/Contents/MacOS/verge-mihomo"
make e2e                                # or: python3 scripts/e2e/live.py
```

Optional:

| Variable | Meaning |
|---|---|
| `E2E_DOWNLOAD_BYTES` | bytes to pull for the accounting check (default 5 MiB) |
| `E2E_DOWNLOAD_URL` | where to pull them from (default speed.cloudflare.com) |
| `E2E_TRAFFIC_PROXY` | proxy name to use for the download (default: first passing non-WireGuard proxy) |
| `E2E_SKIP` | comma-separated proxy names to leave out of the delay test |
| `E2E_HOSTS` | `host=ip,...` pinned for mihomo (machines whose resolver intercepts DNS) |
| `E2E_ACCOUNT_WAIT` | seconds to wait for the charge (default 240; two report intervals plus slack) |

Exit code 1 and a summary on any failure; mihomo's work dir is kept for
inspection. Nothing is created on the panel: the script only reads, and
the download is ordinary traffic on the test user's plan.

Keep a dedicated test user and, ideally, one inbound of every protocol
you ship on the test nodes, so a regression in any core shows up here.
Credentials never go in the repo; a local `.env` sourced before `make
e2e` is the usual arrangement.
