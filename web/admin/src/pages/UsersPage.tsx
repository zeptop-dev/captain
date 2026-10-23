import { ActionIcon, Badge, Button, Code, Drawer, Group, Modal, NumberInput, PasswordInput, Select, Stack, Text, TextInput, Title, Divider } from '@mantine/core'
import { useForm } from '@mantine/form'
import { modals } from '@mantine/modals'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { IconPlus, IconTrash } from '@tabler/icons-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AccountIDEditor } from '../components/AccountIDEditor'
import { useAuth } from '../lib/auth'
import { api, ApiError, type Group as UGroup, type HwidDevice, type OnlineDevice, type Plan, type SubRequest, type UserRow } from '../lib/api'
import { money, when } from '../lib/format'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { Copy } from '../components/Copy'
import { RenewalsPanel } from '../components/RenewalsPanel'
import { UserSubs, type UserSub } from '../components/UserSubs'
import { TempLinks } from '../components/TempLinks'
import { UserEntries } from '../components/UserEntries'
import { HwidDevices } from '../components/HwidDevices'
import { WireGuardDevices } from '../components/WireGuardDevices'
import { UserConnections } from '../components/UserConnections'
import { UserAudit } from '../components/UserAudit'
import { UserGroupsPanel } from '../components/UserGroupsPanel'
import { UserDirectory } from '../components/UserDirectory'
import { UserSubscriptionProfile } from '../components/UserSubscriptionProfile'
import { MetadataEditor } from '../components/MetadataEditor'
import { SegmentedControl } from '@mantine/core'
import { useURLChoice } from '../lib/use-url-choice'

export default function UsersPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const canManageGroups = me?.role === 'admin' || me?.role === 'operator'
  const plans = useQuery({ queryKey: ['plans'], queryFn: () => api.get<Plan[]>('/api/admin/plans') })
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups'), enabled: canManageGroups })
  const [sel, setSel] = useState<UserRow | null>(null)
  const [creating, setCreating] = useState(false)
  const [wireGuardModalOpen, setWireGuardModalOpen] = useState(false)
  const [view, setView] = useURLChoice('tab', canManageGroups ? ['list', 'groups', 'renewals'] : ['list'], 'list')
  const invalidate = () => qc.invalidateQueries({ queryKey: ['users'] })

  const createForm = useForm({
    initialValues: { Email: '', Password: '', ID: '' as number | string },
    validate: { ID: (v) => v === '' || (Number.isSafeInteger(Number(v)) && Number(v) >= 1) ? null : t('accountID.invalid') },
  })
  const create = useMutation({ mutationFn: (v: typeof createForm.values) => api.post<{ id: number }>('/api/admin/users', { Email: v.Email, Password: v.Password, ...(me?.role === 'admin' && v.ID !== '' ? { id: Number(v.ID) } : {}) }), onSuccess: async (created) => {
    toast.ok(t('common.saved')); setCreating(false); createForm.reset(); invalidate()
    // Continue with the newly created user's plan and subscription, without searching again.
    try { open(await api.get<UserRow>(`/api/admin/users/${created.id}`)) } catch (error) { toast.err(error) }
  }, onError: (error) => {
    if (error instanceof ApiError && error.status === 409 && error.message === 'ID is already in use') createForm.setFieldError('ID', t('accountID.inUse'))
    else if (error instanceof ApiError && error.status === 409 && error.message === 'email already exists') createForm.setFieldError('Email', t('users.emailInUse'))
    else toast.err(error)
  } })

  const editForm = useForm({ initialValues: { Status: 'active', GroupID: '', Password: '' } })
  const update = useMutation({ mutationFn: (v: typeof editForm.values) => api.patch(`/api/admin/users/${sel!.id}`, { Status: v.Status, GroupID: v.GroupID ? Number(v.GroupID) : null, Password: v.Password }), onSuccess: () => { toast.ok(t('common.saved')); invalidate() }, onError: toast.err })
  const detail = useQuery({ queryKey: ['user', sel?.id], queryFn: () => api.get<{ devices: OnlineDevice[]; subscriptions: UserSub[]; hwid_devices: HwidDevice[]; hwid_limit: number | null; sub_requests: SubRequest[]; first_connected_at: string | null; dyn_limit: { mbps: number; until: string; rate_mbps: number } | null }>(`/api/admin/users/${sel!.id}`), enabled: sel !== null, refetchInterval: 15000 })
  const [grantPlan, setGrantPlan] = useState<string | null>(null)
  const [grantHow, setGrantHow] = useState<string>('')
  const grant = useMutation({ mutationFn: () => api.post(`/api/admin/users/${sel!.id}/grant`, { PlanID: Number(grantPlan), Activation: grantHow }), onSuccess: () => { toast.ok(t('common.saved')); invalidate(); qc.invalidateQueries({ queryKey: ['user', sel?.id] }); qc.invalidateQueries({ queryKey: ['user-entries', sel?.id] }); setGrantPlan(null) }, onError: toast.err })
  const [delta, setDelta] = useState<number | string>(0)
  const [manualMbps, setManualMbps] = useState<number | string>(10)
  const [manualMin, setManualMin] = useState<number | string>(30)
  const topUp = useMutation({ mutationFn: () => api.post(`/api/admin/users/${sel!.id}/balance`, { DeltaCents: Number(delta) }), onSuccess: () => { toast.ok(t('common.saved')); invalidate(); setSel(null) }, onError: toast.err })
  const rotate = useMutation({ mutationFn: () => api.post<{ sub_token: string; sub_url: string }>(`/api/admin/users/${sel!.id}/rotate-token`), onSuccess: (r) => { toast.ok(t('common.saved')); setSel({ ...sel!, sub_token: r.sub_token, sub_url: r.sub_url }); invalidate() }, onError: toast.err })
  const del = useMutation({ mutationFn: () => api.del(`/api/admin/users/${sel!.id}`), onSuccess: () => { toast.ok(t('common.deleted')); setSel(null); invalidate() }, onError: (e) => toast.err(e instanceof ApiError && e.status === 409 ? t('users.deleteBlocked') : e) })

  const open = (u: UserRow) => { setSel(u); editForm.setValues({ Status: u.status, GroupID: u.group_id ? String(u.group_id) : '', Password: '' }); setGrantPlan(null); setDelta(0) }
  const subURL = sel ? (sel.sub_url || `${window.location.origin}/sub/${sel.sub_token}`) : ''

  return (
    <>
      <PageHeader title={t('users.title')} subtitle={t('users.subtitle')} actions={<><SegmentedControl size="xs" value={view} onChange={setView} data={[{ value: 'list', label: t('users.viewList') }, ...(canManageGroups ? [{ value: 'groups', label: t('users.groups') }] : []), ...(canManageGroups ? [{ value: 'renewals', label: t('users.viewRenewals') }] : [])]} />{view !== 'groups' && canManageGroups && <Button leftSection={<IconPlus size={16} />} onClick={() => setCreating(true)}>{t('users.create')}</Button>}</>} />
      {view === 'groups' && canManageGroups ? <UserGroupsPanel /> : view === 'renewals' ? <RenewalsPanel /> : <UserDirectory onOpen={open} canManage={canManageGroups} plans={plans.data ?? []} groups={groups.data ?? []} />}

      <Modal opened={creating} onClose={() => setCreating(false)} title={t('users.create')}>
        <form onSubmit={createForm.onSubmit((v) => create.mutate(v))}><Stack>
          {me?.role === 'admin' && <NumberInput label={t('accountID.label')} description={t('users.idHint')} inputWrapperOrder={['label', 'input', 'description', 'error']} placeholder={t('users.idAuto')} min={1} max={Number.MAX_SAFE_INTEGER} allowDecimal={false} allowNegative={false} {...createForm.getInputProps('ID')} />}
          <TextInput label={t('users.email')} type="email" required {...createForm.getInputProps('Email')} />
          <PasswordInput label={t('users.password')} required minLength={8} {...createForm.getInputProps('Password')} />
          <Group justify="flex-end"><Button variant="default" onClick={() => setCreating(false)}>{t('common.cancel')}</Button><Button type="submit" loading={create.isPending}>{t('common.create')}</Button></Group>
        </Stack></form>
      </Modal>

      <Drawer opened={sel !== null} onClose={() => setSel(null)} closeOnEscape={!wireGuardModalOpen} closeOnClickOutside={!wireGuardModalOpen} trapFocus={!wireGuardModalOpen} position="right" size="lg" title={sel?.email}>
        {sel && (
          <Stack gap="lg">
            {me?.role === 'admin' && <AccountIDEditor key={`account-id-${sel.id}`} id={sel.id} endpoint={`/api/admin/users/${sel.id}/id`} onChanged={() => setSel(null)} />}
            <Group gap="xl">
              <div><Text size="xs" c="dimmed">{t('users.uuid')}</Text><Group gap={4}><Code>{sel.uuid}</Code><Copy value={sel.uuid} /></Group></div>
              <div><Text size="xs" c="dimmed">{t('users.createdAt')}</Text><Text size="sm">{when(sel.created_at)}</Text></div>
              <div><Text size="xs" c="dimmed">{t('users.firstConnected')}</Text><Text size="sm">{detail.data?.first_connected_at ? when(detail.data.first_connected_at) : t('users.neverConnected')}</Text></div>
            </Group>
            {!detail.data?.dyn_limit && (
              <Group gap="xs" align="flex-end">
                <NumberInput size="xs" w={140} label={t('dynlimit.manual')} min={1} value={manualMbps} onChange={setManualMbps} />
                <NumberInput size="xs" w={140} label={t('dynlimit.manualMinutes')} min={1} value={manualMin} onChange={setManualMin} />
                <Button size="xs" variant="default" onClick={() => api.put(`/api/admin/users/${sel.id}/dyn-limit`, { Mbps: Number(manualMbps), Seconds: Number(manualMin) * 60 }).then(() => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['user', sel.id] }) }).catch(toast.err)}>{t('dynlimit.throttle')}</Button>
              </Group>
            )}
            {detail.data?.dyn_limit && (
              <Group gap="xs">
                <Badge color="orange" variant="light">{t('dynlimit.throttled', { mbps: detail.data.dyn_limit.mbps, rate: detail.data.dyn_limit.rate_mbps, until: when(detail.data.dyn_limit.until) })}</Badge>
                <Button size="compact-xs" variant="subtle" onClick={() => api.del(`/api/admin/users/${sel.id}/dyn-limit`).then(() => { toast.ok(t('common.saved')); qc.invalidateQueries({ queryKey: ['user', sel.id] }) }).catch(toast.err)}>{t('dynlimit.lift')}</Button>
              </Group>
            )}
            <div>
              <Text size="xs" c="dimmed">{t('users.subUrl')}</Text>
              <Group gap={4} wrap="nowrap"><Code style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{subURL}</Code><Copy value={subURL} /></Group>
              <Button size="xs" variant="subtle" color="orange" mt={4} onClick={() => modals.openConfirmModal({ title: t('users.rotate'), children: <Text size="sm">{t('users.rotateHint')}</Text>, labels: { confirm: t('common.confirm'), cancel: t('common.cancel') }, onConfirm: () => rotate.mutate() })}>{t('users.rotate')}</Button>
            </div>
            <div>
              <Text size="xs" c="dimmed">{t('users.devices')}</Text>
              {detail.data?.devices?.length ? <Group gap={6} mt={4}>{detail.data.devices.map((d) => <Badge key={d.ip} variant="light" color={d.via_relay ? 'gray' : 'teal'} title={d.via_relay ? t('users.viaRelay') + ' · ' + when(d.last_seen_at) : when(d.last_seen_at)}>{d.ip}{d.via_relay ? ' · ' + t('users.viaRelay') : ''}</Badge>)}</Group> : <Text size="sm" c="dimmed">{t('users.noDevices')}</Text>}
            </div>
            <Divider />
            <form onSubmit={editForm.onSubmit((v) => update.mutate(v))}><Stack gap="sm">
              <Title order={6}>{t('common.edit')}</Title>
              <Group grow>
                <Select label={t('users.status')} data={[{ value: 'active', label: t('users.active') }, { value: 'banned', label: t('users.banned') }]} allowDeselect={false} {...editForm.getInputProps('Status')} />
                <Select label={t('users.group')} data={[{ value: '', label: t('common.none') }, ...(groups.data ?? []).map((g) => ({ value: String(g.ID), label: g.Name }))]} allowDeselect={false} {...editForm.getInputProps('GroupID')} />
              </Group>
              <PasswordInput label={t('users.newPassword')} {...editForm.getInputProps('Password')} />
              <Group justify="flex-end"><Button type="submit" size="xs" loading={update.isPending}>{t('common.save')}</Button></Group>
            </Stack></form>
            <Divider />
            <Stack gap="sm">
              <Title order={6}>{t('users.grant')}</Title>
              <Text size="xs" c="dimmed">{t('users.grantHint')}</Text>
              <Group align="flex-end"><Select flex={1} data={(plans.data ?? []).map((p) => ({ value: String(p.ID), label: `${p.Name} · ${money(p.PriceCents)}` }))} value={grantPlan} onChange={setGrantPlan} placeholder={t('users.plan')} /><Select w={190} data={[{ value: '', label: t('users.grantDefault') }, { value: 'queue', label: t('users.grantQueue') }, { value: 'replace', label: t('users.grantReplace') }]} value={grantHow} onChange={(v) => setGrantHow(v ?? '')} allowDeselect={false} /><Button size="xs" disabled={!grantPlan} loading={grant.isPending} onClick={() => grant.mutate()}>{t('users.grant')}</Button></Group>
            </Stack>
            <UserSubs userID={sel.id} subs={detail.data?.subscriptions ?? []} onDone={() => setSel(null)} />
            <UserSubscriptionProfile userID={sel.id} subURL={subURL} />
            <WireGuardDevices key={`wireguard-${sel.id}`} endpoint={`/api/admin/users/${sel.id}/wireguard-devices`} canWrite={canManageGroups} titleOrder={6} onModalChange={setWireGuardModalOpen} />
            <MetadataEditor endpoint={`/api/admin/users/${sel.id}/metadata`} />
            <UserEntries userID={sel.id} />
            <TempLinks userID={sel.id} />
            <HwidDevices key={`hwid-${sel.id}`} userID={sel.id} devices={detail.data?.hwid_devices ?? []} limit={detail.data?.hwid_limit ?? null} requests={detail.data?.sub_requests ?? []} />
            <UserConnections userID={sel.id} />
            <UserAudit userID={sel.id} />
            <Stack gap="sm">
              <Title order={6}>{t('users.topUp')}</Title>
              <Text size="xs" c="dimmed">{t('users.topUpHint')} {t('users.balance')}: {money(sel.balance_cents)}</Text>
              <Group align="flex-end"><NumberInput flex={1} value={delta} onChange={setDelta} /><Button size="xs" disabled={!Number(delta)} loading={topUp.isPending} onClick={() => topUp.mutate()}>{t('common.save')}</Button></Group>
            </Stack>
            <Divider />
            <Group justify="flex-end"><Button color="red" variant="light" size="xs" leftSection={<IconTrash size={14} />} onClick={() => modals.openConfirmModal({ title: t('common.delete'), children: <Text size="sm">{t('common.confirmDelete')}</Text>, labels: { confirm: t('common.delete'), cancel: t('common.cancel') }, confirmProps: { color: 'red' }, onConfirm: () => del.mutate() })}>{t('common.delete')}</Button></Group>
          </Stack>
        )}
      </Drawer>
      <ActionIcon style={{ display: 'none' }} />
    </>
  )
}
