import { Button, Card, Divider, Group, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type ACMESettings, type Group as UGroup, type InviteSettings, type NoticeSettings, type OIDCSettings, type SubscriptionSettings } from '../lib/api'
import { JsonInput, NumberInput, Select, Switch } from '@mantine/core'
import { useForm } from '@mantine/form'
import { useEffect } from 'react'
import { PasswordInput, Textarea } from '@mantine/core'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { UpdateCard } from '../components/UpdateCard'
import { MailCard } from '../components/MailCard'
import { RegistrationCard } from '../components/RegistrationCard'
import { ClientsCard, TelegramCard, TrialCard } from '../components/OpsCards'
import { WebhooksCard } from '../components/WebhooksCard'
import { ProbeCard } from '../components/ProbeCard'
import { TokensCard } from '../components/TokensCard'
import { TwoFactorCard } from '../components/TwoFactorCard'
import { SecurityCard } from '../components/SecurityCard'
import { BackupCard } from '../components/BackupCard'
import { KomariCard } from '../components/KomariCard'

export default function SettingsPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const [name, setName] = useState('')
  const create = useMutation({ mutationFn: () => api.post('/api/admin/groups', { Name: name }), onSuccess: () => { toast.ok(t('common.saved')); setName(''); qc.invalidateQueries({ queryKey: ['groups'] }) }, onError: toast.err })
  const subs = useQuery({ queryKey: ['subscription-settings'], queryFn: () => api.get<SubscriptionSettings>('/api/admin/settings/subscription') })
  const [subText, setSubText] = useState<string | null>(null)
  const [shortLinks, setShortLinks] = useState<boolean | null>(null)
  const [autoFlags, setAutoFlags] = useState<boolean | null>(null)
  const [singlePlan, setSinglePlan] = useState<boolean | null>(null)
  const [hwid, setHwid] = useState<{ enabled: boolean; require: boolean; fallback_limit: number; announce: string } | null>(null)
  const hw = hwid ?? subs.data?.hwid ?? { enabled: false, require: false, fallback_limit: 0, announce: '' }
  const saveSubs = useMutation({ mutationFn: (urls: string[]) => api.put('/api/admin/settings/subscription', { URLs: urls, short_links: shortLinks ?? subs.data?.short_links ?? false, auto_flags: autoFlags ?? subs.data?.auto_flags ?? false, single_plan: singlePlan ?? subs.data?.single_plan ?? false, hwid: hw }), onSuccess: () => { toast.ok(t('common.saved')); setSubText(null); qc.invalidateQueries({ queryKey: ['subscription-settings'] }); qc.invalidateQueries({ queryKey: ['users'] }) }, onError: toast.err })
  const invite = useQuery({ queryKey: ['invite-settings'], queryFn: () => api.get<InviteSettings>('/api/admin/settings/invite') })
  const iform = useForm<InviteSettings & { methods: string }>({ initialValues: { enabled: false, percent: 10, first_order_only: false, multi_level: false, level2: 0, level3: 0, payout: 'balance', min_withdraw_cents: 0, withdraw_methods: [], methods: '' } })
  useEffect(() => { if (invite.data) iform.setValues({ ...invite.data, payout: invite.data.payout || 'balance', methods: (invite.data.withdraw_methods ?? []).join(', ') }) }, [invite.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const surplus = useQuery({ queryKey: ['surplus-settings'], queryFn: () => api.get<{ enabled: boolean }>('/api/admin/settings/surplus') })
  const saveSurplus = useMutation({ mutationFn: (enabled: boolean) => api.put('/api/admin/settings/surplus', { enabled }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['surplus-settings'] }) }, onError: toast.err })
  const saveInvite = useMutation({ mutationFn: (v: InviteSettings & { methods: string }) => api.put('/api/admin/settings/invite', { ...v, withdraw_methods: v.methods.split(/[,，\n]/).map((s) => s.trim()).filter(Boolean) }), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['invite-settings'] }) }, onError: toast.err })
  const notice = useQuery({ queryKey: ['notice-settings'], queryFn: () => api.get<NoticeSettings>('/api/admin/settings/notice') })
  const nform = useForm<NoticeSettings>({ initialValues: { enabled: false, title: '', body: '' } })
  useEffect(() => { if (notice.data) nform.setValues(notice.data) }, [notice.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const saveNotice = useMutation({ mutationFn: (v: NoticeSettings) => api.put('/api/admin/settings/notice', v), onSuccess: () => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['notice-settings'] }) }, onError: toast.err })
  const oidc = useQuery({ queryKey: ['oidc'], queryFn: () => api.get<OIDCSettings>('/api/admin/settings/oidc') })
  const [oidcText, setOidcText] = useState<string | null>(null)
  const [pwLogin, setPwLogin] = useState<boolean | null>(null)
  const saveOidc = useMutation({ mutationFn: () => api.put('/api/admin/settings/oidc', { providers: JSON.parse(oidcText ?? JSON.stringify(oidc.data?.providers ?? [])), password_login: pwLogin ?? oidc.data?.password_login ?? true }), onSuccess: () => { toast.ok(t('common.saved')); setOidcText(null); setPwLogin(null); qc.invalidateQueries({ queryKey: ['oidc'] }) }, onError: toast.err })
  const acme = useQuery({ queryKey: ['acme'], queryFn: () => api.get<ACMESettings>('/api/admin/settings/acme') })
  const aform = useForm({ initialValues: { Email: '', CloudflareToken: '' } })
  useEffect(() => { if (acme.data) aform.setFieldValue('Email', acme.data.email) }, [acme.data]) // eslint-disable-line react-hooks/exhaustive-deps
  const saveAcme = useMutation({ mutationFn: (v: { Email: string; CloudflareToken: string }) => api.put('/api/admin/settings/acme', v), onSuccess: () => { toast.ok(t('common.saved')); aform.setFieldValue('CloudflareToken', ''); qc.invalidateQueries({ queryKey: ['acme'] }) }, onError: toast.err })
  return (
    <>
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />
      <Stack>
        <Card>
          <Title order={5} mb="sm">{t('settings.groups')}</Title>
          <Table mb="md"><Table.Tbody>{(groups.data ?? []).map((g) => <Table.Tr key={g.ID}><Table.Td w={60}><Text c="dimmed">#{g.ID}</Text></Table.Td><Table.Td>{g.Name}</Table.Td></Table.Tr>)}</Table.Tbody></Table>
          <Group align="flex-end"><TextInput label={t('settings.groupName')} value={name} onChange={(e) => setName(e.currentTarget.value)} /><Button disabled={!name} loading={create.isPending} onClick={() => create.mutate()}>{t('settings.createGroup')}</Button></Group>
        </Card>
        <Card>
          <Title order={5} mb="xs">{t('settings.subUrls')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.subUrlsHint')}</Text>
          <Textarea autosize minRows={2} placeholder={'https://sub.example.com\nhttps://s[1-9].example.com'} value={subText ?? (subs.data?.urls ?? []).join('\n')} onChange={(e) => setSubText(e.currentTarget.value)} />
          <Switch mt="xs" label={t('settings.shortLinks')} description={t('settings.shortLinksHint')} checked={shortLinks ?? subs.data?.short_links ?? false} onChange={(e) => setShortLinks(e.currentTarget.checked)} />
          <Switch mt="xs" label={t('settings.autoFlags')} description={t('settings.autoFlagsHint')} checked={autoFlags ?? subs.data?.auto_flags ?? false} onChange={(e) => setAutoFlags(e.currentTarget.checked)} />
          <Switch mt="xs" label={t('settings.singlePlan')} description={t('settings.singlePlanHint')} checked={singlePlan ?? subs.data?.single_plan ?? false} onChange={(e) => setSinglePlan(e.currentTarget.checked)} />
          <Divider my="sm" label={t('settings.hwid')} labelPosition="left" />
          <Text size="xs" c="dimmed" mb="xs">{t('settings.hwidHint')}</Text>
          <Switch label={t('settings.hwidEnabled')} checked={hw.enabled} onChange={(e) => setHwid({ ...hw, enabled: e.currentTarget.checked })} />
          {hw.enabled && <>
            <Switch mt="xs" label={t('settings.hwidRequire')} description={t('settings.hwidRequireHint')} checked={hw.require} onChange={(e) => setHwid({ ...hw, require: e.currentTarget.checked })} />
            <Group mt="xs" align="flex-start">
              <NumberInput w={200} label={t('settings.hwidFallback')} description={t('settings.hwidFallbackHint')} min={0} value={hw.fallback_limit} onChange={(v) => setHwid({ ...hw, fallback_limit: Number(v) || 0 })} />
              <TextInput flex={1} label={t('settings.hwidAnnounce')} description={t('settings.hwidAnnounceHint')} value={hw.announce} onChange={(e) => setHwid({ ...hw, announce: e.currentTarget.value })} />
            </Group>
          </>}
          <Group justify="flex-end" mt="sm"><Button size="xs" loading={saveSubs.isPending} disabled={subText === null && shortLinks === null && autoFlags === null && singlePlan === null && hwid === null} onClick={() => saveSubs.mutate((subText ?? '').split('\n').map((l) => l.trim()).filter(Boolean))}>{t('common.save')}</Button></Group>
        </Card>
        <Card>
          <Title order={5} mb="xs">{t('settings.acme')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.acmeHint')}</Text>
          <form onSubmit={aform.onSubmit((v) => saveAcme.mutate(v))}><Stack gap="sm">
            <TextInput label={t('settings.acmeEmail')} placeholder="you@example.com" {...aform.getInputProps('Email')} />
            <PasswordInput label={t('settings.cfToken')} description={acme.data?.has_cloudflare_token ? t('settings.cfTokenSet') : t('settings.cfTokenHint')} placeholder={acme.data?.has_cloudflare_token ? '••••••••' : ''} {...aform.getInputProps('CloudflareToken')} />
            <Group justify="flex-end"><Button type="submit" size="xs" loading={saveAcme.isPending}>{t('common.save')}</Button></Group>
          </Stack></form>
        </Card>
        <Card>
          <Title order={5} mb="xs">{t('settings.notice')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.noticeHint')}</Text>
          <form onSubmit={nform.onSubmit((v) => saveNotice.mutate(v))}><Stack gap="sm">
            <TextInput label={t('settings.noticeTitle')} {...nform.getInputProps('title')} />
            <Textarea label={t('settings.noticeBody')} autosize minRows={2} {...nform.getInputProps('body')} />
            <Group justify="space-between"><Switch label={t('common.enabled')} {...nform.getInputProps('enabled', { type: 'checkbox' })} /><Button type="submit" size="xs" loading={saveNotice.isPending}>{t('common.save')}</Button></Group>
          </Stack></form>
        </Card>
        <Card>
          <Title order={5} mb="xs">{t('settings.invite')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.inviteHint')}</Text>
          <form onSubmit={iform.onSubmit((v) => saveInvite.mutate(v))}><Stack gap="sm">
            <Group grow align="flex-end">
              <NumberInput label={t('settings.invitePercent')} min={0} max={100} {...iform.getInputProps('percent')} />
              <Switch label={t('settings.inviteFirstOnly')} {...iform.getInputProps('first_order_only', { type: 'checkbox' })} />
              <Switch label={t('settings.inviteMulti')} {...iform.getInputProps('multi_level', { type: 'checkbox' })} />
            </Group>
            {iform.values.multi_level && <Group grow>
              <NumberInput label={t('settings.inviteLevel2')} min={0} max={100} {...iform.getInputProps('level2')} />
              <NumberInput label={t('settings.inviteLevel3')} min={0} max={100} {...iform.getInputProps('level3')} />
            </Group>}
            <Group grow align="flex-end">
              <Select label={t('settings.invitePayout')} data={[{ value: 'balance', label: t('settings.payoutBalance') }, { value: 'commission', label: t('settings.payoutCommission') }]} allowDeselect={false} {...iform.getInputProps('payout')} />
              {iform.values.payout === 'commission' && <NumberInput label={t('settings.minWithdraw')} min={0} value={iform.values.min_withdraw_cents / 100} onChange={(v) => iform.setFieldValue('min_withdraw_cents', Math.round(Number(v) * 100))} />}
              {iform.values.payout === 'commission' && <TextInput label={t('settings.withdrawMethods')} placeholder="USDT-TRC20, Alipay" {...iform.getInputProps('methods')} />}
            </Group>
            <Group justify="space-between"><Switch label={t('common.enabled')} {...iform.getInputProps('enabled', { type: 'checkbox' })} /><Button type="submit" size="xs" loading={saveInvite.isPending}>{t('common.save')}</Button></Group>
          </Stack></form>
        </Card>
        <Card>
          <Title order={5} mb="xs">{t('settings.surplus')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.surplusHint')}</Text>
          <Switch label={t('common.enabled')} checked={surplus.data?.enabled ?? false} onChange={(e) => saveSurplus.mutate(e.currentTarget.checked)} />
        </Card>
        <RegistrationCard />
        <TrialCard />
        <MailCard />
        <TelegramCard />
        <WebhooksCard />
        <ProbeCard />
        <KomariCard />
        <TokensCard />
        <TwoFactorCard />
        <SecurityCard />
        <BackupCard />
        <ClientsCard />
        <Card>
          <Title order={5} mb="xs">{t('settings.oidc')}</Title>
          <Text size="xs" c="dimmed" mb="sm">{t('settings.oidcHint')}</Text>
          <JsonInput autosize minRows={4} formatOnBlur value={oidcText ?? JSON.stringify((oidc.data?.providers ?? []).map(({ has_secret: _h, ...p }) => p), null, 2)} onChange={setOidcText} placeholder={'[{"id":"casdoor","name":"Casdoor","issuer":"https://door.example.com","client_id":"...","client_secret":"...","trust_email":true,"auto_register":true}]'} />
          <Group justify="space-between" mt="sm">
            <Switch label={t('settings.passwordLogin')} checked={pwLogin ?? oidc.data?.password_login ?? true} onChange={(e) => setPwLogin(e.currentTarget.checked)} />
            <Button size="xs" loading={saveOidc.isPending} disabled={oidcText === null && pwLogin === null} onClick={() => saveOidc.mutate()}>{t('common.save')}</Button>
          </Group>
          <Text size="xs" c="dimmed" mt="xs">{t('settings.oidcCallback')} <code>{window.location.origin}/api/oauth/&lt;id&gt;/callback</code></Text>
        </Card>
        <UpdateCard />
        <Card><Text size="sm" c="dimmed">{t('settings.version')}: {me?.version ?? '—'}</Text></Card>
      </Stack>
    </>
  )
}
