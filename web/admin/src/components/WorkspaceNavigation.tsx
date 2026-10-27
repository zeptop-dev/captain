import { Link, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../lib/auth'
import { activeNavigationItem, navigationGroup, visibleNavigation, workspaceDestination } from '../lib/navigation'
import classes from './WorkspaceNavigation.module.css'

export function WorkspaceNavigation() {
  const { t } = useTranslation()
  const { me } = useAuth()
  const location = useLocation()
  const group = visibleNavigation(me?.role ?? '').find(group => group.id === navigationGroup(location.pathname))
  if (!group || group.items.length < 2) return null
  const active = activeNavigationItem(group.items, location.pathname, location.search)
  return <nav className={classes.navigation} aria-label={t('workspace.sectionNavigation')}>
    {group.items.map(item => <Link key={item.to} to={workspaceDestination(item, location)} className={classes.link} aria-current={item === active ? 'page' : undefined} onClick={event => {
      if (item === active && item.to.split('?')[0] === location.pathname && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) event.preventDefault()
    }}>{t(item.labelKey)}</Link>)}
  </nav>
}
