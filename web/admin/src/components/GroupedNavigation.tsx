import { NavLink, Stack } from '@mantine/core'
import type { ComponentType } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { NavigationGroup } from '../lib/navigation'
import classes from './GroupedNavigation.module.css'

export function GroupedNavigation({ groups, icons, activeGroup, onNavigate }: {
  groups: NavigationGroup[]
  icons: Record<string, ComponentType<{ size?: number; stroke?: number }>>
  activeGroup?: string
  onNavigate: () => void
}) {
  const { t } = useTranslation()
  return <Stack component="nav" gap={6} aria-label={t('workspace.navigation')}>
    {groups.map(group => {
      const Icon = icons[group.id]
      return <NavLink key={group.id} component={Link} to={group.items[0].to} label={t(group.labelKey)} active={activeGroup === group.id}
        aria-current={activeGroup === group.id ? 'page' : undefined} onClick={onNavigate}
        leftSection={Icon && <Icon size={20} stroke={1.65} />} className={classes.link} />
    })}
  </Stack>
}
