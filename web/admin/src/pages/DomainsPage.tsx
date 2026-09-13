import { ActionIcon, Badge, Button, Card, Code, Drawer, Group, Modal, PasswordInput, Select, Stack, Switch, Table, Tabs, TagsInput, Text, TextInput, Textarea, Tooltip } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconCertificate, IconPencil, IconPlus, IconRefresh, IconTrash, IconWorld } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../lib/api'
import { when } from '../lib/format'
import { toast } from '../lib/notify'
import { Copy } from '../components/Copy'
import { PageHeader } from '../components/PageHeader'

interface Usage { nodes: string[]; tls: string[]; sub_hosts: string[]; panel: boolean }
interface Domain { id: number; name: string; provider: string; has_token: boolean; usage: Usage; certificates: number; created_at: string }
interface Cert { id: number; name: string; domain: string; names: string[]; not_after: string; source: string; issuer: string; renewals: number; last_error: string; auto_renew: boolean; domain_id: number | null; created_at: string; updated_at: string; nodes: string[] }
interface Detail { certificate: Cert & { cert_pem: string }; nodes: string[] }

const daysLeft = (d: string) => Math.floor((new Date(d).getTime() - Date.now()) / 86400e3)

// Domains the operator owns and the certificates for names under them.
// Certificates reach nodes automatically by name coverage.
export default function DomainsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const domains = useQuery({ queryKey: ['domains'], queryFn: () => api.get<{ domains: Domain[]; global_token: boolean; can_issue: boolean }>('/api/admin/domains') })
  const certs = useQuery({ queryKey: ['certificates'], queryFn: () => api.get<{ certificates: Cert[]; webhook_url: string; can_issue: boolean }>('/api/admin/certificates') })
  const refresh = () => { qc.invalidateQueries({ queryKey: ['domains'] }); qc.invalidateQueries({ queryKey: ['certificates'] }) }
  const [tab, setTab] = useState<string | null>('domains')

  // ---- domains ----
  const [editingDomain, setEditingDomain] = useState<Domain | 'new' | null>(null)
  const dform = useForm({ initialValues: { Name: '', Provider: 'cloudflare', CFToken: '' } })
  const saveDomain = useMutation({ mutationFn: (v: typeof dform.values) => editingDomain === 'new' ? api.post('/api/admin/domains', v) : api.patch(`/api/admin/domains/${(editingDomain as Domain).id}`, { Provider: v.Provider, CFToken: v.CFToken }), onSuccess: () => { toast.ok(t('common.saved')); setEditingDomain(null); refresh() }, onError: toast.err })
  const delDomain = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/domains/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); refresh() }, onError: toast.err })
  // The modal's children render even while closed, so never dereference a null selection.
  const hasToken = editingDomain !== null && editingDomain !== 'new' && editingDomain.has_token
  const openDomain = (d: Domain | 'new') => { dform.setValues(d === 'new' ? { Name: '', Provider: 'cloudflare', CFToken: '' } : { Name: d.name, Provider: d.provider, CFToken: '' }); setEditingDomain(d) }
  const useSummary = (u: Usage) => [u.nodes.length && t('domains.useNodes', { n: u.nodes.length }), u.tls.length && t('domains.useTLS', { n: u.tls.length }), u.sub_hosts.length && t('domains.useSub', { n: u.sub_hosts.length }), u.panel && t('domains.usePanel')].filter(Boolean).join(' · ')
  const useDetail = (u: Usage) => [...u.nodes, ...u.tls, ...u.sub_hosts].filter((v, i, a) => a.indexOf(v) === i).join('\n')

  // ---- certificates ----
  const [issuing, setIssuing] = useState(false)
  const iform = useForm<{ Name: string; Names: string[]; DomainID: string }>({ initialValues: { Name: '', Names: [], DomainID: '' } })
  const issue = useMutation({ mutationFn: (v: typeof iform.values) => api.post('/api/admin/certificates/issue', { Name: v.Name, Names: v.Names, DomainID: v.DomainID ? Number(v.DomainID) : null }), onSuccess: () => { toast.ok(t('certs.issued')); setIssuing(false); iform.reset(); refresh() }, onError: toast.err })
  const [uploading, setUploading] = useState(false)
  const uform = useForm({ initialValues: { Name: '', Domain: '', CertPEM: '', KeyPEM: '' } })
  const upload = useMutation({ mutationFn: (v: typeof uform.values) => api.post<Cert>('/api/admin/certificates', v).then((c) => v.Name ? api.patch(`/api/admin/certificates/${c.id}`, { Name: v.Name }) : c), onSuccess: () => { toast.ok(t('common.saved')); setUploading(false); uform.reset(); refresh() }, onError: toast.err })
  const renew = useMutation({ mutationFn: (id: number) => api.post(`/api/admin/certificates/${id}/renew`), onSuccess: () => { toast.ok(t('certs.renewed')); refresh() }, onError: (e: Error) => { toast.err(e); refresh() } })
  const delCert = useMutation({ mutationFn: (id: number) => api.del(`/api/admin/certificates/${id}`), onSuccess: () => { toast.ok(t('common.deleted')); refresh() }, onError: toast.err })
  const meta = useMutation({ mutationFn: (v: { id: number; Name?: string; AutoRenew?: boolean }) => api.patch(`/api/admin/certificates/${v.id}`, { Name: v.Name, AutoRenew: v.AutoRenew }), onSuccess: refresh, onError: toast.err })
  const rotate = useMutation({ mutationFn: () => api.post('/api/admin/certificates/webhook-token'), onSuccess: refresh, onError: toast.err })
  const [viewing, setViewing] = useState<number | null>(null)
  const detail = useQuery({ queryKey: ['certificate', viewing], queryFn: () => api.get<Detail>(`/api/admin/certificates/${viewing}`), enabled: viewing !== null })
  const [renaming, setRenaming] = useState('')
  const canIssue = certs.data?.can_issue ?? domains.data?.can_issue ?? false
  const sourceLabel = (s: string) => s === 'acme' ? "Let's Encrypt" : s === 'webhook' ? 'Webhook' : t('certs.uploaded')
  const expiry = (c: Cert) => { const d = daysLeft(c.not_after); return <Text size="xs" c={d < 0 ? 'red' : d < 14 ? 'red' : d < 30 ? 'orange' : 'dimmed'}>{d < 0 ? t('certs.expired') : t('certs.daysLeft', { n: d })} · {when(c.not_after).split(',')[0]}</Text> }

  return (
    <>
      <PageHeader title={t('domains.title')} subtitle={t('domains.subtitle')} actions={tab === 'domains'
        ? <Button leftSection={<IconPlus size={16} />} onClick={() => openDomain('new')}>{t('domains.add')}</Button>
        : <Group gap="xs"><Button variant="default" onClick={() => setUploading(true)}>{t('certs.upload')}</Button><Button leftSection={<IconPlus size={16} />} disabled={!canIssue} onClick={() => setIssuing(true)}>{t('certs.issue')}</Button></Group>} />
      <Tabs value={tab} onChange={setTab} mb="md">
        <Tabs.List><Tabs.Tab value="domains" leftSection={<IconWorld size={14} />}>{t('domains.tabDomains')}</Tabs.Tab><Tabs.Tab value="certs" leftSection={<IconCertificate size={14} />}>{t('domains.tabCerts')}</Tabs.Tab></Tabs.List>
      </Tabs>

      {tab === 'domains' && (
        <Card p={0}><Table>
          <Table.Thead><Table.Tr><Table.Th>{t('domains.domain')}</Table.Th><Table.Th>DNS</Table.Th><Table.Th>{t('domains.inUse')}</Table.Th><Table.Th>{t('domains.certs')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
          <Table.Tbody>
            {(domains.data?.domains ?? []).map((d) => (
              <Table.Tr key={d.id}>
                <Table.Td><Code>{d.name}</Code></Table.Td>
                <Table.Td>{d.provider === 'cloudflare' ? <Group gap={4}><Badge size="xs" variant="light" color="orange">Cloudflare</Badge><Text size="xs" c="dimmed">{d.has_token ? t('domains.ownToken') : domains.data?.global_token ? t('domains.globalToken') : t('domains.noToken')}</Text></Group> : <Badge size="xs" variant="outline" color="gray">{t('domains.manual')}</Badge>}</Table.Td>
                <Table.Td>{useSummary(d.usage) ? <Tooltip label={<Text size="xs" style={{ whiteSpace: 'pre-line' }}>{useDetail(d.usage)}</Text>} multiline><Text size="sm">{useSummary(d.usage)}</Text></Tooltip> : <Text size="xs" c="dimmed">—</Text>}</Table.Td>
                <Table.Td><Text size="sm">{d.certificates}</Text></Table.Td>
                <Table.Td><Group gap={4} justify="flex-end"><ActionIcon variant="subtle" onClick={() => openDomain(d)}><IconPencil size={16} /></ActionIcon><ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('domains.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => delDomain.mutate(d.id) })}><IconTrash size={16} /></ActionIcon></Group></Table.Td>
              </Table.Tr>
            ))}
            {domains.data?.domains.length === 0 && <Table.Tr><Table.Td colSpan={5}><Text c="dimmed" ta="center" py="lg">{t('domains.empty')}</Text></Table.Td></Table.Tr>}
          </Table.Tbody>
        </Table></Card>
      )}

      {tab === 'certs' && (
        <Stack gap="md">
          <Card p={0}><Table>
            <Table.Thead><Table.Tr><Table.Th>{t('certs.name')}</Table.Th><Table.Th>{t('certs.domains')}</Table.Th><Table.Th>{t('certs.type')}</Table.Th><Table.Th>{t('certs.source')}</Table.Th><Table.Th>{t('certs.expiry')}</Table.Th><Table.Th>{t('certs.renewals')}</Table.Th><Table.Th>{t('certs.deployed')}</Table.Th><Table.Th /></Table.Tr></Table.Thead>
            <Table.Tbody>
              {(certs.data?.certificates ?? []).map((c) => (
                <Table.Tr key={c.id}>
                  <Table.Td><Text size="sm" fw={600}>{c.name}</Text>{c.last_error && <Text size="xs" c="red" maw={260} truncate title={c.last_error}>{c.last_error}</Text>}</Table.Td>
                  <Table.Td><Tooltip label={c.names.join('\n')} multiline disabled={c.names.length < 2}><Group gap={4}><Code>{c.domain}</Code>{c.names.length > 1 && <Badge size="xs" variant="outline" color="gray">+{c.names.length - 1}</Badge>}</Group></Tooltip></Table.Td>
                  <Table.Td><Badge size="xs" variant="light" color={c.names.some((n) => n.startsWith('*.')) ? 'grape' : 'blue'}>{c.names.some((n) => n.startsWith('*.')) ? t('certs.wildcard') : t('certs.single')}</Badge></Table.Td>
                  <Table.Td><Text size="xs">{sourceLabel(c.source)}</Text>{c.source === 'acme' && <Text size="xs" c="dimmed">DNS-01</Text>}</Table.Td>
                  <Table.Td>{expiry(c)}</Table.Td>
                  <Table.Td><Text size="xs">{c.renewals > 0 ? t('certs.renewedTimes', { n: c.renewals }) : '—'}</Text>{c.renewals > 0 && <Text size="xs" c="dimmed">{when(c.updated_at)}</Text>}{c.source === 'acme' && <Text size="xs" c={c.auto_renew ? 'teal' : 'dimmed'}>{c.auto_renew ? t('certs.autoOn') : t('certs.autoOff')}</Text>}</Table.Td>
                  <Table.Td><Tooltip label={c.nodes.join(', ')} disabled={!c.nodes.length}><Text size="sm">{c.nodes.length ? t('certs.nodesCount', { n: c.nodes.length }) : <Text span size="xs" c="dimmed">—</Text>}</Text></Tooltip></Table.Td>
                  <Table.Td><Group gap={4} justify="flex-end" wrap="nowrap">
                    <Button size="compact-xs" variant="subtle" onClick={() => { setViewing(c.id); setRenaming(c.name) }}>{t('certs.view')}</Button>
                    {c.source === 'acme' && <Tooltip label={t('certs.renewNow')}><ActionIcon variant="subtle" loading={renew.isPending && renew.variables === c.id} onClick={() => renew.mutate(c.id)}><IconRefresh size={16} /></ActionIcon></Tooltip>}
                    <ActionIcon variant="subtle" color="red" onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('certs.deleteHint')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => delCert.mutate(c.id) })}><IconTrash size={16} /></ActionIcon>
                  </Group></Table.Td>
                </Table.Tr>
              ))}
              {certs.data?.certificates.length === 0 && <Table.Tr><Table.Td colSpan={8}><Text c="dimmed" ta="center" py="lg">{t('certs.empty')}</Text></Table.Td></Table.Tr>}
            </Table.Tbody>
          </Table></Card>
          {!canIssue && <Text size="xs" c="dimmed">{t('certs.noIssuer')}</Text>}
          <Card>
            <Text size="sm" fw={600}>{t('certs.webhook')}</Text>
            <Text size="xs" c="dimmed" mb={6}>{t('certs.webhookHint')}</Text>
            {certs.data?.webhook_url ? (
              <Group gap={4} wrap="nowrap"><Code style={{ wordBreak: 'break-all' }}>{certs.data.webhook_url}</Code><Copy value={certs.data.webhook_url} /><Tooltip label={t('certs.rotate')}><ActionIcon variant="subtle" size="sm" onClick={() => rotate.mutate()}><IconRefresh size={14} /></ActionIcon></Tooltip></Group>
            ) : <Button size="xs" variant="light" loading={rotate.isPending} onClick={() => rotate.mutate()}>{t('certs.enableWebhook')}</Button>}
            {certs.data?.webhook_url && <Code block mt={6} style={{ fontSize: 11 }}>{'{"domain": "${DOMAINS}", "certificate": "${CERTIFICATE}", "privateKey": "${PRIVATE_KEY}"}'}</Code>}
          </Card>
        </Stack>
      )}

      <Modal opened={editingDomain !== null} onClose={() => setEditingDomain(null)} title={editingDomain === 'new' ? t('domains.add') : t('common.edit')}>
        <form onSubmit={dform.onSubmit((v) => saveDomain.mutate(v))}><Stack>
          <TextInput label={t('domains.domain')} description={t('domains.domainHint')} placeholder="example.com" required disabled={editingDomain !== 'new'} {...dform.getInputProps('Name')} />
          <Select label="DNS" data={[{ value: 'cloudflare', label: 'Cloudflare' }, { value: 'manual', label: t('domains.manual') }]} allowDeselect={false} {...dform.getInputProps('Provider')} />
          {dform.values.Provider === 'cloudflare' && <PasswordInput label={t('domains.token')} description={hasToken ? t('domains.tokenSet') : domains.data?.global_token ? t('domains.tokenGlobalHint') : t('domains.tokenHint')} placeholder={hasToken ? '••••••••' : ''} {...dform.getInputProps('CFToken')} />}
          <Group justify="flex-end"><Button variant="default" onClick={() => setEditingDomain(null)}>{t('common.cancel')}</Button><Button type="submit" loading={saveDomain.isPending}>{t('common.save')}</Button></Group>
        </Stack></form>
      </Modal>

      <Modal opened={issuing} onClose={() => setIssuing(false)} title={t('certs.issue')}>
        <form onSubmit={iform.onSubmit((v) => issue.mutate(v))}><Stack>
          <TextInput label={t('certs.name')} description={t('certs.nameHint')} placeholder={t('certs.namePlaceholder')} {...iform.getInputProps('Name')} />
          <TagsInput label={t('certs.domains')} description={t('certs.domainsHint')} placeholder="example.com, *.example.com" required splitChars={[',', ' ']} {...iform.getInputProps('Names')} />
          <Select label={t('certs.dnsAuth')} description={t('certs.dnsAuthHint')} clearable data={(domains.data?.domains ?? []).filter((d) => d.provider === 'cloudflare').map((d) => ({ value: String(d.id), label: `${d.name}${d.has_token ? '' : ` (${t('domains.globalToken')})`}` }))} {...iform.getInputProps('DomainID')} />
          <Text size="xs" c="dimmed">{t('certs.issueHint')}</Text>
          <Group justify="flex-end"><Button variant="default" onClick={() => setIssuing(false)}>{t('common.cancel')}</Button><Button type="submit" loading={issue.isPending} disabled={iform.values.Names.length === 0}>{t('certs.issueNow')}</Button></Group>
        </Stack></form>
      </Modal>

      <Modal opened={uploading} onClose={() => setUploading(false)} title={t('certs.upload')} size="lg">
        <form onSubmit={uform.onSubmit((v) => upload.mutate(v))}><Stack>
          <Group grow><TextInput label={t('certs.name')} placeholder={t('certs.namePlaceholder')} {...uform.getInputProps('Name')} /><TextInput label={t('certs.primary')} description={t('certs.primaryHint')} placeholder="*.example.com" {...uform.getInputProps('Domain')} /></Group>
          <Textarea label={t('certs.cert')} placeholder="-----BEGIN CERTIFICATE-----" autosize minRows={4} maxRows={8} required styles={{ input: { fontFamily: 'monospace', fontSize: 11 } }} {...uform.getInputProps('CertPEM')} />
          <Textarea label={t('certs.key')} placeholder="-----BEGIN PRIVATE KEY-----" autosize minRows={4} maxRows={8} required styles={{ input: { fontFamily: 'monospace', fontSize: 11 } }} {...uform.getInputProps('KeyPEM')} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setUploading(false)}>{t('common.cancel')}</Button><Button type="submit" loading={upload.isPending}>{t('certs.upload')}</Button></Group>
        </Stack></form>
      </Modal>

      <Drawer opened={viewing !== null} onClose={() => setViewing(null)} position="right" size="lg" title={detail.data?.certificate.name ?? ''}>
        {detail.data && (() => { const c = detail.data.certificate; return (
          <Stack gap="sm">
            <Group align="flex-end"><TextInput label={t('certs.name')} value={renaming} onChange={(e) => setRenaming(e.currentTarget.value)} style={{ flex: 1 }} /><Button size="xs" mb={2} variant="light" disabled={renaming.trim() === c.name || !renaming.trim()} onClick={() => meta.mutate({ id: c.id, Name: renaming.trim() })}>{t('common.save')}</Button></Group>
            <Group gap="xs"><Text size="xs" c="dimmed">ID</Text><Code>cert-{c.id}</Code><Copy value={`cert-${c.id}`} /></Group>
            <div><Text size="xs" c="dimmed">{t('certs.domains')}</Text>{c.names.map((n) => <Code key={n} mr={4}>{n}</Code>)}</div>
            <Group gap="lg"><div><Text size="xs" c="dimmed">{t('certs.source')}</Text><Text size="sm">{sourceLabel(c.source)}{c.issuer ? ` · ${c.issuer}` : ''}</Text></div><div><Text size="xs" c="dimmed">{t('certs.expiry')}</Text>{expiry(c as unknown as Cert)}</div><div><Text size="xs" c="dimmed">{t('certs.renewals')}</Text><Text size="sm">{c.renewals}</Text></div></Group>
            {c.source === 'acme' && <Switch label={t('certs.autoRenew')} description={t('certs.autoRenewHint')} checked={c.auto_renew} onChange={(e) => meta.mutate({ id: c.id, AutoRenew: e.currentTarget.checked })} />}
            {c.last_error && <Text size="xs" c="red">{c.last_error}</Text>}
            <div><Text size="xs" c="dimmed">{t('certs.deployed')}</Text><Text size="sm">{detail.data.nodes.length ? detail.data.nodes.join(', ') : t('certs.noNodes')}</Text></div>
            <div><Group justify="space-between"><Text size="xs" c="dimmed">{t('certs.cert')}</Text><Copy value={c.cert_pem} /></Group><Code block style={{ fontSize: 10, maxHeight: 220, overflow: 'auto' }}>{c.cert_pem}</Code></div>
          </Stack>
        ) })()}
      </Drawer>
    </>
  )
}
