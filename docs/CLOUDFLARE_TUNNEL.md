# Publishing Captain and the bosun panel through Cloudflare Tunnel

Cloudflare Tunnel (cloudflared) attaches an HTTP service to Cloudflare's
edge over an outbound connection, so the machine needs no inbound ports and
no public IP at all. It fits when:

- the panel and the user portal run on a machine without a public address
  (at home, on an internal network, behind NAT);
- you would rather not expose the panel's port to the internet and want it
  reachable only through Cloudflare;
- the same for a standalone bosun panel.

What it **cannot** do, first: a Tunnel forwards HTTP/HTTPS (plus a few
special TCP/UDP cases). **The proxy protocols themselves — VLESS,
Hysteria2, mieru and the rest — cannot go through a Tunnel**, so a node
still needs an address its clients can dial directly. The Tunnel is for the
panel and the subscriptions.

## 1. Prerequisites

1. A domain on Cloudflare.
2. A machine running Captain (or bosun) with outbound internet access.
3. cloudflared installed:

   ```sh
   # Debian / Ubuntu
   curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
   echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main" | sudo tee /etc/apt/sources.list.d/cloudflared.list
   sudo apt update && sudo apt install cloudflared
   ```

## 2. Let Captain serve plain HTTP on loopback

The hop from cloudflared to the panel is local HTTP; Cloudflare terminates
TLS, so Captain must not obtain a certificate of its own:

```yaml
# /etc/captain/config.yaml
listen: 127.0.0.1:8080
base_url: https://panel.example.com     # what users see; subscription links use it
tls:
  auto: false
```

With Docker, `deploy/docker-compose.proxy.yml` does the same: the container
listens on 8080 only.

Restart Captain and check that `curl -s http://127.0.0.1:8080/api/health`
answers.

## 3. Create the tunnel

In the Cloudflare Zero Trust dashboard (Networks → Tunnels) choose "Create
a tunnel", pick cloudflared, give it a name, and you get an install command
like:

```sh
sudo cloudflared service install eyJhIjoi...   # the token
```

Run it on the machine; cloudflared registers itself as a systemd service
and stays up.

Then add one **Public Hostname** to the tunnel:

| Field | Value |
|---|---|
| Subdomain | panel |
| Domain | example.com |
| Type | HTTP |
| URL | 127.0.0.1:8080 |

Cloudflare creates the `panel.example.com` CNAME pointing at the tunnel.
Opening `https://panel.example.com` should show the Captain sign-in page.

The command-line equivalent:

```sh
cloudflared tunnel login
cloudflared tunnel create captain
cloudflared tunnel route dns captain panel.example.com
cat > ~/.cloudflared/config.yml <<EOF
tunnel: captain
credentials-file: /root/.cloudflared/<tunnel-id>.json
ingress:
  - hostname: panel.example.com
    service: http://127.0.0.1:8080
  - service: http_status:404
EOF
sudo cloudflared service install
```

## 4. Subscriptions and the real client address

- Subscription links are built from `base_url`, so they use the same
  hostname and clients fetch them through Cloudflare.
- The login rate limit and the admin allow-list judge the client address.
  Behind Cloudflare the visitor's address arrives in `CF-Connecting-IP`,
  while Captain reads `X-Forwarded-For` (rightmost untrusted entry) and
  falls back to `X-Real-IP`. cloudflared appends `X-Forwarded-For` by
  itself, which is enough; if you want the header spelled out, enable
  **Transform Rules → Managed Transforms → Add "True-Client-IP" header**.
  See [OPERATIONS.md](OPERATIONS.md#admin-access-allow-list-and-client-addresses).
- Cloudflare's free plan limits a single request to 100 seconds. Every
  Captain endpoint is far below that, and the node long-poll
  (`/api/agent/state?wait=`) is capped at 50 seconds for this reason.

## 5. How nodes reach the panel

A node reports over HTTPS like any other client, so it also connects to
`https://panel.example.com` with no extra configuration: the address in the
install command the node page shows is `base_url`.

If you would rather keep node traffic off Cloudflare (to save its bandwidth
or a few milliseconds), give the panel a second address that bypasses the
tunnel — but that needs a public IP, which defeats the point of using a
tunnel in the first place.

## 6. A standalone bosun panel

bosun's own panel listens on `:2053` by default. Add it as another Public
Hostname (Type HTTP, URL `127.0.0.1:2053`), leave the panel domain empty in
its settings so bosun does not try to obtain a certificate, and let
Cloudflare do TLS. Its subscription links (`/sub/<token>`) use the same
hostname.

bosun's login allow-list will see Cloudflare's addresses rather than yours,
so either leave the allow-list empty when you use a tunnel, or fill it with
Cloudflare's ranges.

## 7. Troubleshooting

- **502 on the hostname** — cloudflared cannot reach the local service.
  Check that Captain listens on 127.0.0.1:8080 and read
  `journalctl -u cloudflared`.
- **Redirect loop** — `tls.auto` is still on, or `base_url` says `http`.
  Behind a tunnel the panel must serve plain HTTP and `base_url` must be
  `https`.
- **Signed in, then bounced back to the sign-in page** — `base_url` does
  not match the hostname you actually opened, so the session cookie's
  Secure/host attributes do not line up.
- **A client cannot fetch the subscription** — Cloudflare's Bot Fight Mode
  blocks some clients' User-Agents. Turn it off, or add a WAF rule that
  allows `/sub/*`.
