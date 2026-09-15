# 用 Cloudflare Tunnel 发布 Captain 和 bosun 面板

Cloudflare Tunnel（cloudflared）把一台机器上的 HTTP 服务通过出站连接挂到
Cloudflare 边缘，机器本身不需要开放任何入站端口，也不需要公网 IP。适合：

- Captain 面板和用户门户放在没有公网 IP 的机器上（家里、内网、NAT VPS）。
- 不想把面板端口暴露在公网，只让它通过 Cloudflare 访问。
- bosun 单机面板同理。

先说清楚它**不能**做什么：Tunnel 只转发 HTTP/HTTPS（和少量 TCP/UDP 的
特殊用法）。**代理协议本身的端口（VLESS、Hysteria2、mieru 这些）不能走
Tunnel**，节点还是要有能被客户端直连的地址。Tunnel 只管面板和订阅。

## 1. 准备

1. 域名托管在 Cloudflare。
2. 一台跑 Captain（或 bosun）的机器，能出站访问互联网。
3. 安装 cloudflared：

   ```sh
   # Debian / Ubuntu
   curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
   echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main" | sudo tee /etc/apt/sources.list.d/cloudflared.list
   sudo apt update && sudo apt install cloudflared
   ```

## 2. 让 Captain 只听本机 HTTP

Tunnel 到面板这一段是内网 HTTP，TLS 由 Cloudflare 终止，Captain 自己不要再申请证书：

```yaml
# /etc/captain/config.yaml
listen: 127.0.0.1:8080
base_url: https://panel.example.com     # 用户看到的地址，订阅链接用它
tls:
  auto: false
```

Docker 部署用 `deploy/docker-compose.proxy.yml`，效果一样：容器只听 8080。

重启 Captain 后确认 `curl -s http://127.0.0.1:8080/api/health` 有响应。

## 3. 建隧道

在 Cloudflare Zero Trust 控制台（Networks → Tunnels）点"Create a tunnel"，
选 cloudflared，起个名字，会得到一条安装命令，类似：

```sh
sudo cloudflared service install eyJhIjoi...（token）
```

在机器上执行，cloudflared 会注册为 systemd 服务并常驻。

然后在隧道的 **Public Hostname** 里加一条：

| 字段 | 值 |
|---|---|
| Subdomain | panel |
| Domain | example.com |
| Type | HTTP |
| URL | 127.0.0.1:8080 |

保存后 Cloudflare 会自动创建 `panel.example.com` 的 CNAME 记录，指向隧道。
浏览器打开 `https://panel.example.com` 应该就能看到 Captain 登录页。

命令行方式等价于：

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

## 4. 订阅和真实 IP

- 订阅链接以 `base_url` 生成，走同一个域名，客户端通过 Cloudflare 拉取即可。
- Captain 的登录限流和管理后台白名单按客户端 IP 判断。经过 Cloudflare 后
  真实 IP 在 `CF-Connecting-IP` 头里，Captain 会读取 `X-Forwarded-For` /
  `X-Real-IP`；在 Cloudflare 侧开启 **Transform Rules → Managed Transforms →
  Add "True-Client-IP" header** 或者直接依赖默认的 `X-Forwarded-For` 即可。
- Cloudflare 免费计划对单个请求有 100 秒超时，Captain 的所有接口都远短于此；
  节点长轮询接口 `/api/agent/state?wait=` 也控制在 50 秒内。

## 5. 节点怎么连面板

节点（bosun）向 Captain 上报也是 HTTPS 请求，所以节点同样通过
`https://panel.example.com` 连接，不需要额外配置。配对时给节点的安装命令
里的地址就是 `base_url`。

如果你不希望节点流量也经过 Cloudflare（例如想省 Cloudflare 的带宽或延迟），
可以给节点单独开一个不走 Tunnel 的地址，但那要求机器有公网 IP，和使用
Tunnel 的初衷相悖，一般不需要。

## 6. bosun 单机面板

bosun 的面板默认监听 `:2053`，用同样的方法把它加成 Public Hostname
（Type HTTP，URL `127.0.0.1:2053`）。设置页里"面板域名"留空，不要让
bosun 自己申请证书，TLS 交给 Cloudflare。订阅链接 `/sub/<token>` 也走这个域名。

bosun 的登录白名单会看到 Cloudflare 的地址而不是你的地址，配了 Tunnel
就不要再开白名单，或者只填 Cloudflare 的网段。

## 7. 常见问题

- **打开域名 502**：cloudflared 连不上本机服务，检查 Captain 是否在 127.0.0.1:8080 监听，`journalctl -u cloudflared` 看日志。
- **重定向循环**：`tls.auto` 没关、或者 `base_url` 写成了 http。Tunnel 后面必须是纯 HTTP 且 `base_url` 用 https。
- **登录后跳回登录页**：`base_url` 和实际访问的域名不一致，会话 Cookie 的 Secure 属性对不上。
- **订阅在客户端里拉不下来**：有些客户端的 User-Agent 会被 Cloudflare 的 Bot Fight Mode 拦，关掉该功能或给 `/sub/*` 路径加一条 WAF 放行规则。
