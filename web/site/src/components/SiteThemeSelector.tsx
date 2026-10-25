import { createContext, useContext } from 'react'
import { IconDeviceDesktop, IconMoon, IconSun } from '@tabler/icons-react'
import type { SiteScheme } from '../appearance'

export const SiteSchemeContext = createContext<{ selected: SiteScheme; select: (value: SiteScheme) => void }>({ selected: 'dark', select: () => {} })

export function SiteThemeSelector() {
  const { selected, select } = useContext(SiteSchemeContext)
  const Icon = selected === 'auto' ? IconDeviceDesktop : selected === 'dark' ? IconMoon : IconSun
  return <label className="site-theme-selector" title="自动模式跟随设备系统">
    <Icon size={16} aria-hidden />
    <select aria-label="页面主题" value={selected} onChange={event => select(event.currentTarget.value as SiteScheme)}>
      <option value="auto">自动</option>
      <option value="dark">深色</option>
      <option value="light">浅色</option>
    </select>
  </label>
}
