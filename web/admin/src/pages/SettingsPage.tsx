import { Button, Card, Group, Stack, Table, Text, TextInput, Title } from '@mantine/core'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api, type Group as UGroup } from '../lib/api'
import { useAuth } from '../lib/auth'
import { toast } from '../lib/notify'
import { PageHeader } from '../components/PageHeader'
import { UpdateCard } from '../components/UpdateCard'

export default function SettingsPage() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const qc = useQueryClient()
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.get<UGroup[]>('/api/admin/groups') })
  const [name, setName] = useState('')
  const create = useMutation({ mutationFn: () => api.post('/api/admin/groups', { Name: name }), onSuccess: () => { toast.ok(t('common.saved')); setName(''); qc.invalidateQueries({ queryKey: ['groups'] }) }, onError: toast.err })
  return (
    <>
      <PageHeader title={t('settings.title')} subtitle={t('settings.subtitle')} />
      <Stack>
        <Card>
          <Title order={5} mb="sm">{t('settings.groups')}</Title>
          <Table mb="md"><Table.Tbody>{(groups.data ?? []).map((g) => <Table.Tr key={g.ID}><Table.Td w={60}><Text c="dimmed">#{g.ID}</Text></Table.Td><Table.Td>{g.Name}</Table.Td></Table.Tr>)}</Table.Tbody></Table>
          <Group align="flex-end"><TextInput label={t('settings.groupName')} value={name} onChange={(e) => setName(e.currentTarget.value)} /><Button disabled={!name} loading={create.isPending} onClick={() => create.mutate()}>{t('settings.createGroup')}</Button></Group>
        </Card>
        <UpdateCard />
        <Card><Text size="sm" c="dimmed">{t('settings.version')}: {me?.version ?? '—'}</Text></Card>
      </Stack>
    </>
  )
}
