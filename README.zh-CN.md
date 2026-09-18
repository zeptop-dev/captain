[English](README.md) · **简体中文**

<div align="center">

# Captain

**真正把整个机群管起来的代理面板：用户、套餐、支付、订阅 —— 以及节点本身。**

[![Release](https://img.shields.io/github/v/release/zeptop-dev/captain?style=flat-square&color=brightgreen)](https://github.com/zeptop-dev/captain/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/zeptop-dev/captain/ci.yml?branch=master&style=flat-square)](https://github.com/zeptop-dev/captain/actions)
[![Go](https://img.shields.io/github/go-mod/go-version/zeptop-dev/captain?style=flat-square)](go.mod)
[![Downloads](https://img.shields.io/github/downloads/zeptop-dev/captain/total?style=flat-square)](https://github.com/zeptop-dev/captain/releases)
[![Docker](https://img.shields.io/docker/pulls/zeptop/captain?style=flat-square)](https://hub.docker.com/r/zeptop/captain)
[![License](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)

[安装](#快速开始) · [文档](#文档) · [兼容性](docs/COMPATIBILITY.md) · [更新日志](CHANGELOG.md)

</div>

---

Captain 是单个 Go 二进制文件，管理后台和用户前台都内嵌其中。
它负责把服务卖出去（套餐、订单、八种支付网关、邀请、优惠券、
工单），为用户真正在用的每种客户端生成订阅，并驱动节点：在一台
全新的服务器上粘贴一条命令，节点就出现在后台里，配置齐全，
证书就绪。

节点侧是 [**bosun**](https://github.com/zeptop-dev/bosun)，一个以 root
运行的 agent，它按 Captain 下发的目标状态，把 sing-box、Xray、
mita（mieru）、Hysteria、snell-server 和 realm 作为子进程拉起来。
两者之间的协议有完整文档，且只增字段不改字段，所以节点从来不必跟
面板同步升级。

> [!IMPORTANT]
> 这是一套基础设施，用来给你自己、你的朋友或你的客户运行自己的
> 服务。经由它传输的内容由你负责，你的服务器和用户所在地的法律
> 同样由你负责。部署之前请把这两件事都确认清楚。

## 功能特性

**协议与内核**（[文档](docs/NODES.md#inbounds)）—— VLESS（含 REALITY，带伪装站点和 dest
扫描器）、VMess、Trojan、Shadowsocks（含 2022 加密）、Hysteria2、TUIC、
AnyTLS、mieru、Snell、SOCKS、HTTP、NaiveProxy 和 WireGuard。每个入站
各自选择内核；二进制由 bosun 安装并托管，因此没有任何魔改分支需要
维护。

**贴合客户端的订阅**（[文档](docs/SUBSCRIPTIONS.md)）—— mihomo/Clash、Stash、sing-box、
Egern、Surge、Surfboard、Loon、Quantumult X、base64 分享链接（v2rayN、
Shadowrocket）以及 WireGuard `.conf`，每种格式都严格按该客户端自己的
官方文档生成：它表达不了的节点就干脆不下发。支持按格式定制模板、
带 ACL4SSR 规则库的可视化代理组与规则编辑器、节点名变量
（`{{DAYS_LEFT}}`、`{{TRAFFIC_LEFT}}` 等）、信息行、短链接，以及可
吊销的临时链接。

**计费**（[文档](docs/BILLING.md)）—— 套餐可设周期、流量配额、设备数和限速；一个用户可以
同时持有多个套餐（叠加、排队、替换）；订单支持余额、易支付
（v1 MD5 与 v2 RSA）、Stripe、支付宝当面付、Coinbase Commerce、
CoinPayments、BTCPay Server 和 MGate 付款。每个回调都先验签再读取
字段，结算是幂等的，金额与订单不符的回调一律拒绝。还有优惠券、
礼品码、带提现的邀请返佣，以及升级套餐时的剩余价值抵扣。

**机群**（[文档](docs/NODES.md)）—— 一行命令装好节点并完成配对；入站带一键配置模板；
入口（entries）决定每组用户看到什么；端口转发（内置中转、nftables
DNAT 或 realm）可组成中转链，并在每一跳之间传递 PROXY protocol；
面向 IPLC 的线路入口；出口跟随入口；按节点限速；节点任务（升级、
回滚、REALITY 扫描、测速）全部从后台下发。

**经过验证的加固，不是嘴上说说**（[文档](docs/ADMIN.md#access-control-and-the-client-address)）—— 内核以非特权账号
运行，只授予它们真正需要的 capabilities；节点自身的地址空间
（loopback、link-local、云厂商元数据、RFC 1918）在内核路由和 nftables
出站防护两处都被拒绝；内核那些不带鉴权的控制 API 只有 root 能访问。
写 cookie 时校验 Origin，密码用 argon2id，管理人员支持 TOTP，API token
可设只读范围和有效期，后台 IP 白名单同样覆盖 MCP 端点，每个节点还有
doctor 体检，直接报出到底哪里不对。

**运维**（[文档](docs/MONITORING.md)）—— 探针页面带每个节点的历史记录和告警、Prometheus
指标、连接日志、全站审计规则与自动封禁、跨节点统一计算的动态限速、
HWID 设备识别、`/sub` 上的响应规则、每天 `VACUUM INTO` 备份并上传
WebDAV/S3、面板和所有节点的自更新、Telegram 机器人和事件 webhook，
还有一个 MCP server，让 agent 替你回答“哪个节点挂了”。

**六种语言**，两套界面都支持 —— 简体中文、繁體中文、English、日本語、
Русский、한국어 —— 发给用户的邮件还能在「设置 → 邮件」里单独选
语言。

## 截图

<details open>
<summary>管理后台与用户前台</summary>

| | |
|---|---|
| **总览** —— 机群、流量、收入 | **节点** —— 一台机器，一个 bosun |
| ![Overview](docs/img/overview.png) | ![Nodes](docs/img/nodes.png) |
| **单个节点** —— 主机指标、内核、入站 | **入口** —— 用户看到的内容，带标签和地区 |
| ![Node](docs/img/node.png) | ![Entries](docs/img/entries.png) |
| **单个用户** —— 套餐、设备、限速、订阅 | **前台** —— 客户拿到的界面 |
| ![User](docs/img/user.png) | ![Portal](docs/img/portal.png) |

</details>

## 快速开始

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh
```

安装脚本会询问面板域名和管理员账号，有 Docker 就用 Docker，没有就
装成 systemd 服务，申请证书，最后打印出后台地址和登录凭据。用管道
交给 `sh` 执行时，脚本自身不会落盘。

非交互方式，每个选项都用参数给出：

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh -s -- \
  --mode docker --domain panel.example.com --email you@example.com \
  --admin-email you@example.com --admin-password 'a-long-secret'
```

然后在 `https://panel.example.com/admin/` 登录，添加一个节点，把节点
页面给出的命令粘贴到一台全新的服务器上执行：

```sh
curl -fsSL https://panel.example.com/api/agent/install.sh?pair=CODE | sh
```

这条命令会安装 bosun、与面板配对并应用配置。用户在 `/portal/` 打开
前台，订阅地址也在那里。

卸载安装脚本装的所有东西：

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh -s -- uninstall
```

Docker Compose、裸机部署、HTTPS 方案和 Cloudflare Tunnel
见 [docs/DEPLOY.md](docs/DEPLOY.md)。

## 支持的平台

|  | 面板（Captain） | 节点（bosun） |
|---|---|---|
| Linux amd64 / arm64 | ✅ systemd、OpenRC 或 Docker | ✅ systemd、OpenRC（Alpine）或 Docker |
| 其他 Unix | 能编译，但不支持 | 必须是 Linux：要用 nftables、tc 和 capabilities |
| 反向代理之后 | ✅ Caddy、nginx、1Panel、cloudflared | — |

如果通过 Cloudflare Tunnel 对外发布，面板自己不需要开放任何公网端口
（[docs/CLOUDFLARE_TUNNEL.md](docs/CLOUDFLARE_TUNNEL.md)）。

## 数据库

SQLite，而且只有 SQLite —— 没有 Postgres，没有 MySQL，没有 Redis。
这是量过之后的决定，不是图省事：仓库里有可复现的基准测试
（`internal/store/scale_test.go`），一次给 1 000 个用户扣量的节点上报
耗时 85 ms；折算到 50 个节点、50 000 个用户的规模，单连接的占用率是
7 %。每一张会随流量增长的表都有上限。具体数字、天花板在哪里，以及
什么情况下这个结论会变，见
[docs/COMPATIBILITY.md](docs/COMPATIBILITY.md#database-sqlite-only-and-what-that-is-good-for)。

备份是每天一次的 `VACUUM INTO` 快照，可以直接拷贝、gzip 压缩和恢复；
恢复、升级和回滚的步骤见
[docs/OPERATIONS.md](docs/OPERATIONS.md)。

## 配置

`/etc/captain/config.yaml`（完整示例：[config.example.yaml](config.example.yaml)）。
启动时遇到不认识的配置项会直接报错，所以拼错一个键不会悄悄变成默认值。

| 配置项 | 作用 | 默认值 |
|---|---|---|
| `base_url` | 面板对外的 origin；用于生成链接、Origin 校验和 ACME | — |
| `listen` | 监听地址 | `:8080` |
| `tls.auto`、`tls.email` | 为面板自身申请证书 | 关闭 |
| `database.dsn` | SQLite 文件 | `/var/lib/captain/captain.db` |
| `agent.pull_seconds`、`agent.push_seconds` | 节点拉取状态和上报的间隔 | 60 / 60 |
| `trusted_proxies` | 转发头可信的代理 | 回环 + 内网 |
| `admin_allow_cidrs` *(设置项)* | 允许访问后台和 `/mcp` 的地址 | 不限制 |
| `min_version` | 自更新和回滚的版本下限 | 未设置 |
| `payments.*` | 支付网关凭据 | 默认全部关闭 |

## 文档

| | |
|---|---|
| [DEPLOY.md](docs/DEPLOY.md) | 安装脚本、Docker、裸机、HTTPS、订阅域名 |
| [BILLING.md](docs/BILLING.md) | 套餐、支付网关、优惠券、礼品码、邀请、注册限制 |
| [SUBSCRIPTIONS.md](docs/SUBSCRIPTIONS.md) | 格式与模板、入口、节点名变量、响应规则、HWID、短链接 |
| [NODES.md](docs/NODES.md) | 节点与入站、中转与端口转发、线路入口、出口、证书 |
| [MONITORING.md](docs/MONITORING.md) | 探针与状态页、告警、指标、连接日志、审计规则、动态限速 |
| [ADMIN.md](docs/ADMIN.md) | 管理人员角色、API token、访问控制、webhook、Telegram、邮件、OIDC、备份 |
| [OPERATIONS.md](docs/OPERATIONS.md) | 备份、恢复、升级、回滚、该监控什么、客户端地址 |
| [COMPATIBILITY.md](docs/COMPATIBILITY.md) | 一个版本承诺了什么、bosun 版本对应表、数据库上限 |
| [ARCHITECTURE.md](docs/ARCHITECTURE.md) | 代码包、领域模型、agent 协议、任务 |
| [CLOUDFLARE_TUNNEL.md](docs/CLOUDFLARE_TUNNEL.md) | 没有公网 IP，不开任何端口 |
| [MCP.md](docs/MCP.md) | Model Context Protocol 服务端及其工具 |
| [bosun](https://github.com/zeptop-dev/bosun) | 节点 agent：内核、转发、证书、隔离 |

## 版本策略

从 1.0.0 起遵循语义化版本。agent 协议只会新增字段，所以比面板旧的
节点照样能用；订阅 URL、管理 API、webhook 载荷和配置项在 1.x 内保持
稳定；数据库迁移只向前，不回退。细节以及 Captain ↔ bosun 的版本
对应表见 [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md)。

每次发布都跑和 CI 一样的检查（i18n 一致性、oxlint、gofmt、vet、
`go test -race`）；凡是动到节点协议、订阅输出或资金链路的版本，还要
在真实节点和真实客户端上跑一遍线上回归（`scripts/e2e/README.md`）。

## 从源码构建

需要 Go 1.26 和 pnpm：

```sh
make build            # builds web/admin, web/portal, web/site and web/probe, then the binary with them embedded
cp config.example.yaml /etc/captain/config.yaml       # set base_url
bin/captain admin create -c /etc/captain/config.yaml -email you@example.com -password '...'
bin/captain serve -c /etc/captain/config.yaml
```

`make test` 跑 Go 测试；`internal/http/e2e_test.go` 会对着一个临时
数据库把整套 API 端到端跑一遍。

## 参与贡献

欢迎提 issue 和 pull request。提交之前请先跑 `make test` 和
`python3 scripts/i18n-check.py web/admin/src/i18n web/portal/src/i18n`。
涉及安全的问题请开私有安全公告（private advisory），不要开公开 issue。

## 致谢

没有它驱动的这些内核，Captain 就毫无意义：
[sing-box](https://github.com/SagerNet/sing-box)、
[Xray-core](https://github.com/XTLS/Xray-core)、
[mieru](https://github.com/enfein/mieru)、
[Hysteria](https://github.com/apernet/hysteria)、
[snell-server](https://manual.nssurge.com/others/snell.html) 和
[realm](https://github.com/zhboner/realm)；还有它要适配的那些客户端，
尤其是 [mihomo](https://github.com/MetaCubeX/mihomo)；证书部分用的是
[certmagic](https://github.com/caddyserver/certmagic)。
规则库来自 [ACL4SSR](https://github.com/ACL4SSR/ACL4SSR)。
在功能设计上给过启发的前人项目：[Xboard](https://github.com/cedar2025/Xboard)、
[3x-ui](https://github.com/MHSanaei/3x-ui) 和
[Remnawave](https://github.com/remnawave/panel)。

## 许可证

MIT —— 见 [LICENSE](LICENSE)。

<details>
<summary>Star 历史</summary>

[![Star History Chart](https://api.star-history.com/svg?repos=zeptop-dev/captain&type=Date)](https://star-history.com/#zeptop-dev/captain&Date)

</details>
