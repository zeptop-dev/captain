import { useTranslation } from 'react-i18next'
import { createContext, useContext } from 'react'
import { IconMoon, IconSun } from '@tabler/icons-react'
import type { SiteScheme } from '../appearance'

export const SiteSchemeContext = createContext<{ selected: SiteScheme; select: (value: SiteScheme) => void }>({ selected: 'light', select: () => {} })

export function SiteThemeSelector() {
  const { t } = useTranslation()
  const { selected, select } = useContext(SiteSchemeContext)
  const Icon = selected === 'dark' ? IconMoon : IconSun
  return <button type="button" className="site-theme-selector" aria-label={selected === 'dark' ? t('common.switchLight') : t('common.switchDark')} onClick={() => select(selected === 'dark' ? 'light' : 'dark')}>
    <Icon size={18} aria-hidden />
  </button>
}
