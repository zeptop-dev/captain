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

Captain v1.22 / bosun v0.70 新增 NextTrace 一键安装、工具与 ICMP 权限检查，以及显示实际阶段的升级进度条。

节点管理新增共用分组与排序：支持拖拽、上下移动、批量改组和并发保存检查，公开探针同步沿用。无需升级 bosun。 地区下拉框支持按机器公网 IP 自动识别、手动选择国旗与代码，或不显示地区。[分组与排序](docs/NODES.md#groups-and-display-order)。

Captain v1.21 / bosun v0.69 新增管理员专用的三网回程探测，以及独立采集的机器公网 IPv4/IPv6。可按地址族启用自动跟随，既有固定公网入口保持不变。[节点监控与地址](docs/NODES.md)。

Captain v1.20 / bosun v0.68 新增托管反向 WireGuard：B 主动建立隧道，A 鉴权计费，内网按网段、协议和端口授权，由 B 执行 SNAT。同一 A 上普通出口与多条 WG 共享用户限速。 [WireGuard](docs/NODES.md#managed-reverse-wireguard-captain-120--bosun-068).

**v1.28 / v0.75 的 WG 改进**：新建连接默认对双栈域名优先使用 IPv4，并明确启用 B 的 UFW
转发自动管理。旧连接保留原选路与手动规则，编辑时可主动开启。两项设置分别要求 A/B
上报新鲜能力；不支持的防火墙、路由及关闭的全局自动放行会明确报错。规则同步成功与
隧道握手均不代表客户端实际可用。[支持范围与地址族限制](docs/NODES.md#automatic-forwarding-on-b)。

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

下一版支持添加“仅监控”节点，不下载和运行代理内核，保留监控、告警和节点升级；可在节点详情切换为完整节点。面板与节点两端都阻止代理配置，已有节点保持原行为。详见[节点模式](docs/NODES.md#monitor-only-nodes-captain-118--bosun-066)。

## 功能特性

- 客户端管理提供官方版本检查、可选安装包缓存和明确发布；按平台设置推荐项及首页/用户中心展示，保留自定义链接。Apple 客户端跳转官方页面或商店，检查更新不会自动替换已发布版本。[客户端下载](docs/CLIENT_DOWNLOADS.md)。
- 全站网页品牌支持浅/深色 Logo、favicon 与浏览器标题，供首页、公开服务状态、门户、认证页和后台共用。服务器探针可选择跟随主站或使用独立品牌，切换时保留独立资料；上传图片随数据库备份保存，网页名称不改写邮件和通知配置。[品牌设置](docs/ADMIN.md#theme-and-page-injection)。
- 用户登录和门户统一首页的明暗风格，提供紧凑的主题与六语言按钮、浏览器主题记忆及移动端底栏。每个套餐分别显示余量、重置和到期，排队套餐可展开；连接、订阅、设备、认证和支付继续沿用既有流程。[用户门户](docs/BILLING.md#the-portal)。
- 用户门户将各套餐余量、到期时间和连接入口放在总览，连接、账单、帮助和账户各有独立入口，手机使用底部导航。登录、注册和找回密码统一使用站点品牌、主题与语言切换。下载、支持的订阅导入、二维码和可搜索节点集中在连接页；支付回跳以真实订单状态为准。[用户门户](docs/BILLING.md#the-portal)。
- 官网首页与公开服务状态页采用统一导航、响应式布局、默认浅色并可通过小图标切换深浅的主题、首次跟随浏览器语言的六语选择及本地地理示意图；连接流程随主题配色，当前浏览器记住手动选择，与后台、门户及服务器探针分开保存，不随账户同步。继续使用已配置的自定义品牌内容、套餐、注册策略和下载链接。[外观设置](docs/ADMIN.md#theme-and-page-injection)。
- 内置三网监控支持 TCP 或 ICMP，以及 IPv4、IPv6 或独立双栈历史；各模式保留自己的目标，不可达/无权限与真正超时分别显示，Glass 分开展示 v4/v6 色条。需要 bosun v0.82.0 或更新版本；保留已有明确目标，空列表使用 NextTrace 北京目标。[三网监控](docs/MONITORING.md)。
- 服务状态页按展示分类汇总订阅节点；门户只显示用户有权使用的节点，管理员可另行选择公开节点和维护时段。它独立于服务器探针，运行状态不等于端到端可用性；确认配置已应用需要 bosun v0.81.0 或更新版本。[服务状态说明](docs/MONITORING.md#customer-service-status)。
- Glass 费用汇总支持统一币种，服务端每日缓存汇率，服务器详情仍保留原价。缺少报价和旧缓存明确提示，不向汇率服务发送服务器资料。[费用估值](docs/MONITORING.md#probe-and-status-page)。
- 管理员订阅预设统一提供基础、同步的 ACL4SSR 官方配置和另存设计；地区组内可自动选择低延迟节点或手动固定节点，优先使用登记地区并省略未被使用的空地区。地区分组、Claude 增强与 WebRTC/UDP 443 限制适配七种设计器格式，DNS 和具体规则能力按客户端说明限制；预览后仅应用支持的格式。载入前可查看缓存的上游说明及来源版本，Claude 固定快照支持差异预览和按客户端记录发布版本；同步规则库、保存草稿均不会修改已发布模板。[预设流程与边界](docs/SUBSCRIPTIONS.md#templates-and-the-designer)。
- 服务器列表与详情分别展示网卡周期流量和今日用户代理流量（UTC）。托管反向 WireGuard / VLESS Reverse 由前置 A 采集、归属落地 B 展示，保留采集来源和历史不完整提示，用户账本仍只计一次。bosun v0.76.1 起默认避免将托管 WireGuard、整形网卡重复计入主机汇总。[流量统计说明](docs/MONITORING.md#probe-and-status-page)。
- 在用户门户或用户详情中为每台设备建立独立 WireGuard 身份，复制仅含 WG 的设备订阅，或下载单节点配置。各设备共用所属用户的用量和限制，可单独撤销。请使用 bosun v0.77.1 或更新版本，并明确激活受支持的 Xray `26.7.28-r3` 包；默认内核版本不会自动更换。[设备订阅与限制](docs/SUBSCRIPTIONS.md#independent-wireguard-devices)。
- 探针卡片展示 CPU 规格和可选存储空间，Glass 卡片将标称带宽放到剩余天数下方，详情归入网络信息。月流量上限可直接选择 MB、GB 或 TB；存储支持自动推荐，或手动选择、命名和排序公开卷，不公开挂载路径。Captain v1.34 为每个公开卷提供独立容量历史图表。完整 CPU 拓扑及 mergerfs 采集需要 bosun ≥ v0.76.0，旧节点仍然兼容。[探针监控说明](docs/MONITORING.md)。
- 中转可明确选择服务器资料中已登记的内网 IP，无需重复填写。显示实际目标地址，从前置服务器检查 TCP，并在修改地址时同步更新引用线路；已有路径和订阅连接地址保留原默认行为。[内网中转地址](docs/NODES.md#reusing-a-servers-internal-address)。
- 在同一页面管理订阅节点与可复用线路：创建直连节点、发布 relay/nft/realm 转发路径，或将两个订阅节点组成受支持的客户端链。服务器详情展示关联配置与实际上报状态。线路支持 TCP 主备及加权候选，并明确传输协议和宿主防火墙的限制。[线路](docs/NODES.md#managed-server-lines) · [客户端链](docs/SUBSCRIPTIONS.md#managed-server-lines-and-client-chains)。
- 订阅节点支持独立展示分类，可单个或批量归类，并在后台和用户门户筛选。分类名称、说明与顺序不改变权限、计费、标签或订阅节点顺序。[节点分类](docs/SUBSCRIPTIONS.md#entries-what-each-user-sees)。
- 节点域名冲突检查与显式共享／外部 DNS 模式防止误覆盖记录；DNS 健康检查、持续异常告警和自动变更历史帮助定位问题，并按已保存的旧值手动恢复。[DNS 运维说明](docs/MONITORING.md)。
- 在节点详情下载已审核的内核包并激活指定版本（bosun ≥ 0.64）。官方与 Extended sing-box 可同时运行；SSH TCP 代理订阅带有持久主机公钥验证。[内核管理与支持范围](docs/NODES.md#managing-node-core-packages)。
- Captain v1.27 / bosun v0.74 将私网策略能力与 nft 运行状态分开报告，提供前提检查并自动判定防护方式；仅内核授权须管理员明确确认，继续支持指定地址、协议和端口。[自动防护方式](docs/NODES.md#automatic-protection-mode)。
- 管理员可逐入站授权私网目的地址，限定 CIDR、协议和端口，其他入站保持默认隔离；托管 Reverse 在落地端按连接独立执行。需要 bosun ≥ 0.63 及支持此功能的 Linux nft、sing-box/Xray 配置。[使用方式与支持范围](docs/NODES.md#per-inbound-private-access-captain-114--bosun-063)。
- 节点路由支持管理员配置限定端口、协议的私网上游例外（需要 bosun ≥ 0.62）；转发状态区分 TCP 探测与未检测的 UDP，nft 支持同地址族 IPv6 转发。
- 后台常用页面直接显示，低频工具收进“更多”；首页优先展示节点异常和日常待办，并直达对应筛选列表。系统设置集中为八个可直接编辑的分类，支持跨分类搜索、独立保存、草稿保护和响应式表单，以及用户到订阅、节点到入口的直接衔接。[导航与设置说明](docs/ADMIN.md#finding-a-task)。
- 运维管理新增细粒度 API 令牌、员工 Passkey、用户筛选与批量预览操作、内部元数据、命名订阅模板和展示方案、结构化配置预设，以及供应商和节点续费台账。[使用说明与边界](docs/ADMIN_WORKFLOWS.md)。
- 流量接收和入账在同一事务内完成，配合 bosun 持久化重试批次；新增 IPv4/IPv6、ASN/地区和可选服务检测的结构化出口诊断（bosun >= 0.57.0）。


删除节点时，可保留 bosun 并转为独立模式、卸载 bosun（可选保留数据），
或仅删除面板记录。远程操作需要 bosun ≥ v0.55.0，详见
[节点删除说明](docs/NODES.md#removing-a-node)。

**协议与内核**（[文档](docs/NODES.md#inbounds)）—— VLESS（含 REALITY，带伪装站点和 dest
扫描器）、VMess、Trojan、Shadowsocks（含 2022 加密与 ShadowTLS v3）、Hysteria2、TUIC、
AnyTLS、mieru、Snell、SOCKS、HTTP、SSH TCP 代理、NaiveProxy 和 WireGuard。每个入站
各自选择内核；bosun 安装并以独立进程托管经过审核的内核二进制，
也可选用 sing-box Extended 发行版。
入站内核选项按协议和功能筛选，支持自动选择预览、节点可用性提示，并分别展示指定内核和运行内核。

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
DNAT 或 realm）可组成中转链，在每一跳之间传递 PROXY protocol，
还能配置备用目标或按权重分摊的多个目标；
VLESS Reverse 向导一次配置多台中转（中转鉴权计费、逐中转检测 SNI、落地主动连接）；
bosun v0.61.0 起支持 REALITY 的 TLS 记录长度筛查，旧节点显示未检测；
NAT / IPLC 入口与端口映射（入站、转发统一校验，支持多段和单端口映射）；出口跟随入口；按节点限速；节点任务（升级、
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
HWID 设备识别、`/sub` 上的响应规则、每天 `VACUUM INTO` 备份并用 age
加密后连同配置文件上传 WebDAV/S3、仪表盘上的面板自检、面板和所有节点的自更新、Telegram 机器人和事件 webhook，
还有一个 MCP server，让 agent 替你回答“哪个节点挂了”。

下一版资源监控提供逐网卡筛选、文件系统与 inode、磁盘 I/O、逻辑 CPU 和受管进程详情，并支持单独关闭公开状态页；需要 bosun ≥ 0.56。网络质量支持失败原因、P50/P95、抖动及 DNS/连接/TLS/响应分段耗时，并按实际探测次数统计历史。监控工作台支持独立分组、筛选、四节点对比和逐设备平均值/峰值历史；公开状态页支持六种语言、显示内容配置和紧凑布局。告警工作台支持持久化事件、确认与恢复、维护／静默窗口，以及带观测覆盖率的节点联系可用率。节点详情还可按需运行 DNS、服务连通性、下载吞吐和 MTR／路由追踪，限制执行资源并保留最近结果。详见[监控文档](docs/MONITORING.md)。

还可按节点开启 NVIDIA／AMD GPU 监控，展示利用率、显存、温度和功耗，缺失与过期读数明确标示，私有历史按实际采样去重。公开状态页提供极光、纸面、终端、玻璃拟态主题及明亮／深色／跟随系统模式，也可继续继承全站主题。玻璃拟态采用 Komari Glassmorphism 原版公开界面，包含卡片／列表、延迟与丢包色条、详情图及可选本地地球效果。DStatus／Komari 外部监控对接已移除，自有监控和此主题继续保留。


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

后台员工账号和普通用户在同一个数据库中分表存放，独立编号，新安装的第一个普通用户从 1 开始。
管理员只管理后台；购买套餐或使用订阅需要另建普通用户。旧数据的迁移规则见
[账号说明](docs/ADMIN.md#staff-roles)。
管理员可在用户详情和员工编辑窗口中修改对应的数字 ID，订阅链接、凭据和账号历史会保留。
“设置 → 重置整站”可清空业务数据和后台设置，保留当前管理员登录和部署文件。
执行前请查看[重置范围及节点清理说明](docs/ADMIN.md#reset-site)。

在「用户管理 → 更多 → 注册与试用 → 注册限制」控制公开注册。
“开放注册”总开关保存后立即生效；关闭时也会禁止外部登录自动建号，已有用户仍可登录。

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

Docker 部署可启用独立的宿主机升级服务，在网页中一键更新镜像。已有部署用安装脚本的
`upgrade` 命令升级并保留配置，新装 Docker 可加 `--web-upgrade`。
详见[升级与卸载](docs/LIFECYCLE.md)。

卸载安装脚本装的程序、配置和数据（共享 Docker 和其他应用保留）：

```sh
curl -fsSL https://raw.githubusercontent.com/zeptop-dev/captain/master/install.sh | sh -s -- uninstall --yes
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

### 主机管理

防火墙显示自动监听规则的来源和同步状态；共享端口在最后一个引用删除后才撤销，手动及无法确认归属的旧规则保留（需要 bosun v0.72.0+）。

Captain 节点详情提供实际状态、单项修改及操作记录。原生 root Debian/Ubuntu systemd 节点支持受控管理；SSH、DNS、防火墙变更带本地超时恢复。其他环境只读，缺少的系统软件不会自动安装。

节点探针支持金额、币种与付款周期分开填写，日历选择到期日，以及流量按到期日同日／次日每月重置。可选“在线自动续期”：到期后收到新的资源上报，按账期顺延，并在短月份保留原账单日；仅维护探针记录，不代付服务商账单。旧价格文本和手动重置设置保持兼容，详见[监控文档](docs/MONITORING.md#node-cost-and-expiry-linked-traffic-reset)。

Captain v1.28 / bosun v0.75 增加 WG 两端依赖检查与管理员确认后的系统包安装，加快首次 IP 与 DNS 同步。明确确认的仅内核私网授权可保留 WARP、代理和负载均衡分流；双层防护仍只支持直连出站。

节点探针可记录服务器标称带宽，支持 Mbps／Gbps、双向一致或分别填写 RX／TX，并可选择在两套公开探针主题中展示。仅用于资料展示，不修改限速，也不代表测速结果。详见[标称带宽](docs/MONITORING.md#advertised-server-bandwidth)。

Captain 可通过已登记的 OAuth 应用向 New API 提供普通用户登录。使用 New API 自定义 OAuth，Scopes 配置为 `profile email`；详见[接入步骤与边界](docs/CAPTAIN_LOGIN.md)。
