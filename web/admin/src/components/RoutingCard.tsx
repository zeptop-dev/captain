import { MultiSelect, TagsInput, ActionIcon, Badge, Button, Card, Code, Group, Select, Stack, Text, TextInput, Textarea, Title, Box } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, runNodeJob } from '../lib/api'
import { toast } from '../lib/notify'
import { bytes } from '../lib/format'
import { WarpCard, warpTemplate, type WarpAccount } from './WarpCard'

interface Remote { host: string; port: number; uuid?: string; password?: string; username?: string; settings: { protocol: string } }
interface Outbound { tag: string; protocol?: string; settings?: Record<string, unknown>; proxy_tag?: string; remote?: Remote; warp?: { from_node?: boolean }; balancer?: { members: string[]; strategy?: string } }
interface Rule { match: string[]; action: string; value?: string }
interface Routing { outbounds: Outbound[]; routes: Rule[]; default_outbound: string; dns?: string[]; traffic?: Record<string, { today: number; total: number }> }

// Landing outbounds and route rules for one node: paste a share link to add
// an exit, then send everything (default) or specific inbounds to it.
const geoCategories = ['netflix', 'disney', 'openai', 'anthropic', 'google', 'youtube', 'telegram', 'twitter', 'facebook', 'apple', 'microsoft', 'github', 'spotify', 'tiktok', 'category-ads-all', 'cn', 'geolocation-!cn', 'private']
const geoCountries = ['cn', 'us', 'jp', 'hk', 'tw', 'sg', 'kr', 'gb', 'de', 'ru', 'private']

export function RoutingCard({ nodeID, inboundTags, embedded }: { nodeID: number; inboundTags: string[]; embedded?: boolean }) {
  // Embedded inside the node page's advanced section: no card frame, no title.
  const Root = embedded ? Box : Card
  const { t } = useTranslation()
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['routing', nodeID], queryFn: () => api.get<Routing>(`/api/admin/nodes/${nodeID}/routing`) })
  const [nr, setNr] = useState<Routing>({ outbounds: [], routes: [], default_outbound: '', dns: [] })
  const [bal, setBal] = useState<{ tag: string; members: string[]; strategy: string }>({ tag: 'lb', members: [], strategy: 'urltest' })
  useEffect(() => { if (q.data) setNr({ outbounds: q.data.outbounds ?? [], routes: q.data.routes ?? [], default_outbound: q.data.default_outbound ?? '', dns: q.data.dns ?? [] }) }, [q.data])
  const save = useMutation({ mutationFn: (v: Routing) => api.put(`/api/admin/nodes/${nodeID}/routing`, v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['routing', nodeID] }); qc.invalidateQueries({ queryKey: ['node', String(nodeID)] }) }, onError: toast.err })
  const [link, setLink] = useState('')
  const [tag, setTag] = useState('')
  const parse = useMutation({ mutationFn: () => api.post<{ nodes: { name: string; protocol: string; remote: Remote }[] }>('/api/admin/external/parse', { Text: link }), onSuccess: (r) => {
    if (!r.nodes.length) { toast.err(new Error(t('routing.badLink'))); return }
    const n = r.nodes[0]
    const tg = (tag || n.name || n.protocol).replace(/[^\w.-]+/g, '-')
    setNr((cur) => ({ ...cur, outbounds: [...cur.outbounds, { tag: tg, remote: n.remote }] }))
    setLink(''); setTag('')
  }, onError: toast.err })
  const tags = nr.outbounds.map((o) => o.tag)
  const outboundOptions = [{ value: 'direct', label: t('routing.direct') }, { value: 'block', label: t('routing.block') }, ...tags.map((x) => ({ value: x, label: x }))]
  const describe = (o: Outbound) => o.balancer ? `${t('routing.balancer')}: ${o.balancer.members.join(', ')}` : o.warp ? 'Cloudflare WARP' : o.remote ? `${o.remote.settings.protocol} ${o.remote.host}:${o.remote.port}` : `${o.protocol} (${t('routing.raw')})`
  const addWarpOutbound = () => setNr((cur) => cur.outbounds.some((o) => o.warp) ? cur : { ...cur, outbounds: [...cur.outbounds, { tag: 'warp', warp: { from_node: true } }] })
  const addWarpTemplate = (keys: string[]) => setNr((cur) => { const tag = cur.outbounds.find((o) => o.warp)?.tag ?? 'warp'; const have = new Set(cur.routes.flatMap((r) => r.match)); const rules = warpTemplate.filter((g) => keys.includes(g.key)).map((g) => ({ match: g.domains.map((d) => `domain:${d}`).filter((m) => !have.has(m)), action: 'outbound', value: tag })).filter((r) => r.match.length); return { ...cur, routes: [...cur.routes, ...rules] } })
  return (
    <Root mb={embedded ? 0 : "lg"}>
      {!embedded && <Title order={5} mb={4}>{t('routing.title')}</Title>}
      <Text size="xs" c="dimmed" mb="sm">{t('routing.hint')}</Text>
      <Stack gap="xs">
        {nr.outbounds.map((o, i) => (
          <Group key={i} justify="space-between" wrap="nowrap">
            <Group gap="xs" wrap="nowrap" style={{ minWidth: 0 }}><Code>{o.tag}</Code><Text size="sm" truncate>{describe(o)}</Text>{q.data?.traffic?.[o.tag] && <Text size="xs" c="dimmed">{bytes(q.data.traffic[o.tag].today)} / {bytes(q.data.traffic[o.tag].total)}</Text>}{o.proxy_tag && <Badge size="xs" variant="light">{t('routing.via', { tag: o.proxy_tag })}</Badge>}{nr.default_outbound === o.tag && <Badge size="xs" color="teal">{t('routing.isDefault')}</Badge>}</Group>
            <Group gap={4} wrap="nowrap">
              <Select size="xs" w={150} placeholder={t('routing.chain')} data={[{ value: '', label: t('routing.noChain') }, ...tags.filter((x) => x !== o.tag).map((x) => ({ value: x, label: t('routing.via', { tag: x }) }))]} value={o.proxy_tag ?? ''} allowDeselect={false} onChange={(v) => setNr((cur) => ({ ...cur, outbounds: cur.outbounds.map((x, j) => (j === i ? { ...x, proxy_tag: v || undefined } : x)) }))} />
              <ActionIcon variant="subtle" color="red" onClick={() => setNr((cur) => ({ ...cur, outbounds: cur.outbounds.filter((_, j) => j !== i), routes: cur.routes.filter((r) => r.value !== o.tag), default_outbound: cur.default_outbound === o.tag ? '' : cur.default_outbound }))}><IconTrash size={14} /></ActionIcon>
            </Group>
          </Group>
        ))}
        <Group align="flex-end" wrap="nowrap">
          <TextInput label={t('routing.addFromLink')} placeholder="vless://… / ss://… / socks://user:pass@host:port" style={{ flex: 3 }} value={link} onChange={(e) => setLink(e.currentTarget.value)} />
          <TextInput label={t('routing.tag')} placeholder="exit-us" style={{ flex: 1 }} value={tag} onChange={(e) => setTag(e.currentTarget.value)} />
          <Button size="xs" mb={2} variant="light" leftSection={<IconPlus size={14} />} disabled={!link.trim()} loading={parse.isPending} onClick={() => parse.mutate()}>{t('routing.add')}</Button>
        </Group>
        <Textarea label={t('routing.rawJSON')} description={t('routing.rawHint')} autosize minRows={2} ff="monospace" value={JSON.stringify(nr.outbounds.filter((o) => !o.remote && !o.warp && !o.balancer), null, 0)} onBlur={(e) => { try { const raw = JSON.parse(e.currentTarget.value || '[]') as Outbound[]; setNr((cur) => ({ ...cur, outbounds: [...cur.outbounds.filter((o) => o.remote), ...raw] })) } catch { toast.err(new Error('invalid JSON')) } }} />
        {!(false) && (
          <Group align="flex-end" wrap="nowrap">
            <TextInput label={t('routing.balancerTag')} placeholder="lb" style={{ flex: 1 }} value={bal.tag} onChange={(e) => setBal({ ...bal, tag: e.currentTarget.value })} />
            <MultiSelect label={t('routing.balancerMembers')} description={t('routing.balancerHint')} style={{ flex: 2 }} data={tags.filter((x) => !nr.outbounds.find((o) => o.tag === x)?.balancer)} value={bal.members} onChange={(v) => setBal({ ...bal, members: v })} />
            <Select label={t('routing.balancerStrategy')} w={140} data={[{ value: 'urltest', label: t('routing.strategyUrltest') }, { value: 'random', label: t('routing.strategyRandom') }]} allowDeselect={false} value={bal.strategy} onChange={(v) => setBal({ ...bal, strategy: v ?? 'urltest' })} />
            <Button size="xs" mb={2} variant="light" disabled={!bal.tag.trim() || bal.members.length === 0 || tags.includes(bal.tag.trim())} onClick={() => { setNr((cur) => ({ ...cur, outbounds: [...cur.outbounds, { tag: bal.tag.trim(), balancer: { members: bal.members, strategy: bal.strategy } }] })); setBal({ tag: 'lb', members: [], strategy: 'urltest' }) }}>{t('routing.addBalancer')}</Button>
          </Group>
        )}
        <TagsInput label={t('routing.dns')} description={t('routing.dnsHint')} placeholder="1.1.1.1, tls://1.1.1.1, https://dns.google/dns-query"  value={nr.dns ?? []} onChange={(v) => setNr((cur) => ({ ...cur, dns: v }))} />
        <WarpCard queryKey={['node-warp', nodeID]} load={async () => (await api.get<{ status?: { warp?: WarpAccount | null } }>(`/api/admin/nodes/${nodeID}`)).status?.warp ?? null} register={(license) => runNodeJob<WarpAccount>(nodeID, 'warp_register', { license })} hasOutbound={nr.outbounds.some((o) => o.warp)} onAddOutbound={addWarpOutbound} onAddTemplate={addWarpTemplate} />
        <Group grow align="flex-end">
          <Select label={t('routing.default')} description={t('routing.defaultHint')} data={[{ value: '', label: t('routing.direct') }, ...tags.map((x) => ({ value: x, label: x }))]} value={nr.default_outbound} allowDeselect={false} onChange={(v) => setNr((cur) => ({ ...cur, default_outbound: v ?? '' }))} />
        </Group>
        <Text size="sm" fw={600} mt="xs">{t('routing.rules')}</Text>
        {nr.routes.map((r, i) => (
          <Group key={i} align="flex-end" wrap="nowrap">
            <TextInput label={i === 0 ? t('routing.match') : undefined} description={i === 0 ? t('routing.matchHint') : undefined} style={{ flex: 3 }} value={r.match.join(', ')} onChange={(e) => setNr((cur) => ({ ...cur, routes: cur.routes.map((x, j) => (j === i ? { ...x, match: e.currentTarget.value.split(',').map((s) => s.trim()).filter(Boolean) } : x)) }))} />
            <Select label={i === 0 ? t('routing.category') : undefined} placeholder="geosite / geoip" w={170} searchable  data={[{ group: 'geosite', items: geoCategories.map((c) => ({ value: `geosite:${c}`, label: c })) }, { group: 'geoip', items: geoCountries.map((c) => ({ value: `geoip:${c}`, label: c })) }]} value={null} onChange={(v) => v && setNr((cur) => ({ ...cur, routes: cur.routes.map((x, j) => (j === i && !x.match.includes(v) ? { ...x, match: [...x.match, v] } : x)) }))} />
            <Select label={i === 0 ? t('routing.to') : undefined} style={{ flex: 1 }} data={outboundOptions} value={r.action === 'outbound' ? r.value ?? '' : r.action} allowDeselect={false} onChange={(v) => setNr((cur) => ({ ...cur, routes: cur.routes.map((x, j) => (j === i ? (v === 'direct' || v === 'block' ? { match: x.match, action: v } : { match: x.match, action: 'outbound', value: v ?? '' }) : x)) }))} />
            <ActionIcon variant="subtle" color="red" mb={4} onClick={() => setNr((cur) => ({ ...cur, routes: cur.routes.filter((_, j) => j !== i) }))}><IconTrash size={14} /></ActionIcon>
          </Group>
        ))}
        <Group justify="space-between">
          <Group gap="xs">
            <Button size="xs" variant="light" leftSection={<IconPlus size={14} />} onClick={() => setNr((cur) => ({ ...cur, routes: [...cur.routes, { match: [], action: 'direct' }] }))}>{t('routing.addRule')}</Button>
            {inboundTags.map((ib) => <Button key={ib} size="xs" variant="subtle" onClick={() => setNr((cur) => ({ ...cur, routes: [...cur.routes, { match: [`inbound:${ib}`], action: tags[0] ? 'outbound' : 'direct', value: tags[0] }] }))}>{t('routing.routeInbound', { tag: ib })}</Button>)}
          </Group>
          <Button size="xs" loading={save.isPending} onClick={() => save.mutate(nr)}>{t('common.save')}</Button>
        </Group>
      </Stack>
    </Root>
  )
}
