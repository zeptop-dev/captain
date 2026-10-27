import { Card, Group, SimpleGrid, Text } from '@mantine/core'
import { IconArrowRight } from '@tabler/icons-react'
import { Link, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { canVisit } from '../lib/navigation'
import { PageHeader } from '../components/PageHeader'

export default function BusinessPage() {
  const { section } = useParams()
  const { t } = useTranslation()
  const { me } = useAuth()
  const content = section === 'content'
  const entries = content ? [
    { to: '/articles', labelKey: 'nav.articles' }, { to: '/settings/category/site#site', labelKey: 'nav.site' }, { to: '/settings/category/site#notice', labelKey: 'settings.notice' },
  ] : [
    { to: '/coupons', labelKey: 'nav.coupons' }, { to: '/gifts', labelKey: 'nav.gifts' }, { to: '/withdrawals', labelKey: 'nav.withdrawals' }, { to: '/settings/category/business#invite', labelKey: 'settings.invite' },
  ]
  return <><PageHeader title={t(content ? 'workspace.content' : 'workspace.marketing')} subtitle={t('workspace.businessHint')} /><SimpleGrid cols={{ base: 1, sm: 2 }}>{entries.filter(e => canVisit(e.to, me?.role ?? '')).map(e => <Card component={Link} to={e.to} key={e.to} style={{ textDecoration: 'none', color: 'inherit' }}><Group justify="space-between"><Text fw={600}>{t(e.labelKey)}</Text><IconArrowRight size={17} /></Group></Card>)}</SimpleGrid></>
}
