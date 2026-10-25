const messages = {
  "serviceStatus": {
    "title": "服务状态",
    "serverMonitoring": "服务器监控",
    "scope": "查看已公开的订阅节点及其接入路径运行状态。",
    "refresh": "刷新",
    "all": "全部",
    "unavailable": "暂时无法获取当前状态",
    "fetchError": "更新失败，已隐藏旧状态。请稍后重试。",
    "offline": "当前网络已断开，已隐藏旧状态。恢复连接后将重新获取。",
    "expired": "状态报告已超过有效期，所有节点暂按状态未知显示。",
    "never": "尚无记录",
    "overview": "节点运行概况",
    "allHealthy": "运行正常",
    "issues": "部分异常",
    "allDown": "节点故障",
    "maintaining": "维护中",
    "awaiting": "状态待确认",
    "emptyTitle": "暂无公开节点",
    "updated": "最近更新：{{time}}",
    "autoRefresh": "每 60 秒刷新",
    "empty": "当前没有公开展示的订阅节点。",
    "nodes": "节点状态",
    "categories": "节点分类",
    "categoryHint": "分类数字为运行正常数 / 总数；点击节点查看检测依据。",
    "observed": "最近观测：{{time}}",
    "noEndToEnd": "端到端访问：尚未检测",
    "limitations": "仅汇总已公开节点的入站、转发与隧道运行信号，不保证客户端到目标网站的完整访问。没有流量不视为故障。",
    "states": {
      "healthy": "运行正常",
      "degraded": "部分异常",
      "down": "故障",
      "maintenance": "维护中",
      "unknown": "状态未知"
    },
    "reasons": {
      "healthy": "必要组件的运行信号正常。",
      "no_data": "尚未收到足够的运行数据。",
      "stale": "上报已过期，等待新的观测。",
      "config_pending": "配置等待应用，暂时无法确认运行状态。",
      "component_failure": "必要组件报告故障。",
      "no_route": "当前没有可用的接入路径。",
      "partial_failure": "仍有可用路径，部分候选或备用路径故障。",
      "partial_unknown": "仍有可用路径，部分候选路径状态未知。",
      "unsupported": "当前数据或能力不足以判断状态。",
      "external_path": "路径包含未托管的转发，暂时无法确认整条路径。",
      "maintenance": "管理员正在维护此节点。"
    },
    "evidenceLabel": "检测依据",
    "evidence": {
      "inbound": "入站运行",
      "forward_tcp": "下一跳 TCP 探测",
      "wireguard": "WireGuard 隧道",
      "vless_reverse": "VLESS 反向连接",
      "client_chain": "客户端链依赖"
    },
    "closed": "服务状态页暂未公开",
    "closedHint": "你可以登录用户中心查看自己可用节点的状态。"
  },
  "servers": {
    "uncategorized": "未分类"
  }
}
export function statusText(key: string, options?: { time?: string; defaultValue?: string }): string {
  let value: unknown = messages
  for (const part of key.split('.')) value = value && typeof value === 'object' ? (value as Record<string, unknown>)[part] : undefined
  return typeof value === 'string' ? value.replace('{{time}}', options?.time ?? '') : options?.defaultValue ?? messages.serviceStatus.reasons.unsupported
}
