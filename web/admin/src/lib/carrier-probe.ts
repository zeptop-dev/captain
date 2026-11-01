export type CarrierProtocol = 'tcp' | 'icmp'
export type CarrierFamily = 'ipv4' | 'ipv6'
export type CarrierTargetKey = `${CarrierProtocol}_${CarrierFamily}`
export interface CarrierTarget { name: string; addr: string }
export interface CarrierProbe {
  tcp_province?: string
  protocol: CarrierProtocol
  family: CarrierFamily | 'dual'
  tcp_ipv4: CarrierTarget[]
  tcp_ipv6: CarrierTarget[]
  icmp_ipv4: CarrierTarget[]
  icmp_ipv6: CarrierTarget[]
}
export const carrierTargetKeys: CarrierTargetKey[] = ['tcp_ipv4', 'tcp_ipv6', 'icmp_ipv4', 'icmp_ipv6']
export type CarrierTargetTexts = Record<CarrierTargetKey, string>

export function carrierProbeDraft(value?: Partial<CarrierProbe>, legacy: CarrierTarget[] = []) {
  const probe: CarrierProbe = { tcp_province: 'bj', protocol: 'tcp', family: 'ipv4', tcp_ipv4: legacy, tcp_ipv6: [], icmp_ipv4: [], icmp_ipv6: [], ...value }
  probe.tcp_province ||= 'bj'
  const texts = Object.fromEntries(carrierTargetKeys.map(key => [key, (probe[key] ?? []).map(c => `${c.name} ${c.addr}`).join('\n')])) as CarrierTargetTexts
  return { probe, texts }
}

export function parseCarrierTargets(text: string): CarrierTarget[] {
  return text.split('\n').map(line => line.trim()).filter(Boolean).map(line => {
    const [name, ...rest] = line.split(/[\s,=]+/)
    return { name, addr: rest.join('') }
  })
}

export function carrierProbePayload(probe: CarrierProbe, texts: CarrierTargetTexts): CarrierProbe {
  return { ...probe, ...Object.fromEntries(carrierTargetKeys.map(key => [key, parseCarrierTargets(texts[key])])) }
}

export function carrierTargetPlaceholder(protocol: CarrierProtocol, family: CarrierFamily, province = 'bj'): string {
  return [['CT', '4134'], ['CU', '4837'], ['CM', '9808']].map(([name, asn]) => `${name} ${protocol === 'tcp' ? `${province || 'bj'}-${name.toLowerCase()}-${family === 'ipv4' ? 'v4' : 'v6'}.ip.zstaticcdn.com:80` : `${family}.pek-${asn}.endpoint.nxtrace.org`}`).join('\n')
}
