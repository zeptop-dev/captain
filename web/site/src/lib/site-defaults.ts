import type { Site } from './site'

// These are the exact defaults from internal/http/site/site.go. Edited content
// belongs to the operator: do not trim, loosely match, or translate by title.
const defaultTagline = '稳定、快速、省心的网络加速'
const defaultDescription = '多地区节点，自动优选线路，一条订阅链接覆盖所有设备。'
const defaultFeatures = [
  { title: '多协议', text: 'VLESS、Hysteria2、mieru 等主流协议，客户端一键导入。', key: 'protocols' },
  { title: '全球节点', text: '覆盖多个地区，按需选择延迟最低的线路。', key: 'locations' },
  { title: '隐私优先', text: '不记录访问内容，仅统计流量用量。', key: 'privacy' },
  { title: '多设备', text: '手机、电脑、路由器共用一个订阅。', key: 'devices' },
  { title: '自动更新', text: '节点变动自动同步到订阅，无需手动操作。', key: 'updates' },
  { title: '随时支持', text: '遇到问题随时联系我们。', key: 'support' },
] as const
const defaultFAQ = { q: '支持哪些客户端？', a: 'Clash 系、sing-box、Shadowrocket、Surge、v2rayN 等，导入订阅链接即可。' }

export function localizeSiteDefaults(site: Site, translate: (key: string) => string): Site {
  return {
    ...site,
    tagline: site.tagline === defaultTagline ? translate('home.defaults.tagline') : site.tagline,
    description: site.description === defaultDescription ? translate('home.defaults.description') : site.description,
    features: site.features.map(feature => {
      const original = defaultFeatures.find(item => item.title === feature.title && item.text === feature.text)
      return original ? { ...feature, title: translate(`home.defaults.${original.key}.title`), text: translate(`home.defaults.${original.key}.text`) } : feature
    }),
    faq: site.faq.map(item => item.q === defaultFAQ.q && item.a === defaultFAQ.a
      ? { ...item, q: translate('home.defaults.faq.q'), a: translate('home.defaults.faq.a') }
      : item),
  }
}
