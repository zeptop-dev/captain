import { Alert, Button, Stack, Text } from '@mantine/core'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { useNodeAccess } from '../lib/node-access-query'
import { useAuth } from '../lib/auth'
import type { InboundAccess, NodeAccessDirectory } from '../lib/node-access'

export function AccessSummary({ row, directory }: { row?: InboundAccess; directory?: NodeAccessDirectory }) {
  const { t } = useTranslation()
  const names = directory?.plans.filter(plan => row?.plan_ids.includes(plan.id)).map(plan => plan.name) ?? []
  const label = !row ? t('nodeAccess.unknown') : names.length ? t('planSelection.authorized', { plans: names.join(' / ') }) : t('planSelection.unassigned')
  return <Text size="xs" c="dimmed" maw={260} lineClamp={2} title={label}>{label}</Text>
}

export function InboundAccessControl({ inboundID }: { inboundID: number }) {
  const { t } = useTranslation()
  const { me } = useAuth()
  const query = useNodeAccess()
  if (me?.role !== 'admin') return null
  const row = query.isError ? undefined : query.data?.inbounds.find(inbound => inbound.inbound_id === inboundID)
  return <Stack gap={4}>
    <AccessSummary row={row} directory={query.data} />
    <Text size="xs" c="dimmed">{t('planSelection.manageHint')}</Text>
    {row && row.entries.length > 1 && <Text size="xs" c="orange">{t('nodeAccess.sharedHint')} {row.entries.map(entry => entry.name).join(' / ')}</Text>}
    <Button component={Link} to="/plans" size="compact-xs" variant="subtle" w="fit-content">{t('plans.title')}</Button>
  </Stack>
}

export function CategoryAccessNotice({ categoryID }: { categoryID: number }) {
  const { t } = useTranslation()
  const query = useNodeAccess()
  const groups = query.data?.groups.filter(group => group.category_ids?.includes(categoryID)).map(group => group.id) ?? []
  const names = query.data?.plans.filter(plan => plan.group_id !== null && groups.includes(plan.group_id)).map(plan => plan.name) ?? []
  if (!names.length) return null
  return <Alert color="yellow"><Text size="xs">{t('planSelection.followWarning')}</Text><Text size="xs">{t('planAccess.impact')}: {names.join(' / ')}</Text></Alert>
}
