import { ActionIcon } from '@mantine/core'
import { IconMoon, IconSun } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { usePortalAppearance } from '../lib/theme'

export function ThemeToggle() {
  const { t } = useTranslation()
  const { scheme, toggle } = usePortalAppearance()
  return <ActionIcon className="portal-theme-toggle" variant="subtle" color="gray" onClick={toggle} aria-label={scheme === 'dark' ? t('portalAppearance.toLight') : t('portalAppearance.toDark')}>
    {scheme === 'dark' ? <IconMoon size={19} stroke={1.6} aria-hidden /> : <IconSun size={19} stroke={1.6} aria-hidden />}
  </ActionIcon>
}
