import { createContext, useContext } from 'react'
import { IconMoon, IconSun } from '@tabler/icons-react'
import type { SiteScheme } from '../appearance'

export const SiteSchemeContext = createContext<{ selected: SiteScheme; select: (value: SiteScheme) => void }>({ selected: 'light', select: () => {} })

export function SiteThemeSelector() {
  const { selected, select } = useContext(SiteSchemeContext)
  const Icon = selected === 'dark' ? IconMoon : IconSun
  return <button type="button" className="site-theme-selector" aria-label={selected === 'dark' ? '切换为浅色' : '切换为深色'} onClick={() => select(selected === 'dark' ? 'light' : 'dark')}>
    <Icon size={18} aria-hidden />
  </button>
}
