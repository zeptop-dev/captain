import { useContext } from 'react'
import { Icon } from '@iconify/react'
import { t } from '@shared/i18n'
import { snapshot } from '@shared/captain'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { ScrolledContext } from '@/components/Provider'
import { useBox } from '@/hooks/useBox'
import { useVisitorAudit } from '@/hooks/useVisitorAudit'
import { useRoute, useRouter } from '@/lib/router'
import { appStore, useApp } from '@/stores/app'
import LanguageSwitcher from '@/components/LanguageSwitcher'

// VisitorInfo is not rendered: Captain sets visitorInfoEnabled to false and
// the status page must not run the original theme's visitor IP lookup.

export default function Header() {
  const router = useRouter()
  const route = useRoute()
  const { record: recordVisitorEvent } = useVisitorAudit()
  const isScrolled = useContext(ScrolledContext)
  const snap = useBox(snapshot)
  const st = useApp(s => ({
    managedThemeMode: s.managedThemeMode,
    isBeijingDaytime: s.isBeijingDaytime,
    isDark: s.isDark,
    themeMode: s.themeMode,
    homeToolsEnabled: s.homeToolsEnabled,
    homeAdvancedToolsVisible: s.homeAdvancedToolsVisible,
    loading: s.loading,
    privateFeaturesAllowed: s.privateFeaturesAllowed,
    hideAdminEntryWhenLoggedOut: s.hideAdminEntryWhenLoggedOut,
    sitename: s.publicSettings?.sitename,
  }))

  const siteFavicon = snap?.logo || ''

  const themeTitleMap = {
    auto: st.managedThemeMode === 'beijing'
      ? st.isBeijingDaytime ? '自动主题：北京时间日间' : '自动主题：北京时间夜间'
      : st.managedThemeMode === 'light' ? '自动主题：后台浅色' : '自动主题：后台深色',
    light: t('浅色主题'),
    dark: t('深色主题'),
  } as const

  const themeIconMap = {
    auto: st.isDark ? 'icon-park-outline:moon' : 'icon-park-outline:sun-one',
    light: 'icon-park-outline:sun-one',
    dark: 'icon-park-outline:moon',
  } as const

  const buttons: Array<{ title: string, icon: string, action: string, pressed?: boolean }> = []
  if (route.name === 'home' && st.homeToolsEnabled) {
    buttons.push({
      title: st.homeAdvancedToolsVisible ? t('收起首页工具') : t('显示首页工具'),
      icon: 'tabler:tools',
      action: 'toggleHomeTools',
      pressed: st.homeAdvancedToolsVisible,
    })
  }
  buttons.push({
    title: `${themeTitleMap[st.themeMode]}（点击切换）`,
    icon: themeIconMap[st.themeMode],
    action: 'toggleTheme',
  })
  if (!st.loading && (st.privateFeaturesAllowed || !st.hideAdminEntryWhenLoggedOut)) {
    buttons.push({
      title: t('后台管理'),
      icon: 'icon-park-outline:setting',
      action: 'jumpToSetting',
    })
  }

  function handleButtonClick(action: string) {
    switch (action) {
      case 'toggleTheme':
        appStore.updateThemeMode()
        void recordVisitorEvent({
          event: 'theme_mode_change',
          path: router.currentRoute.path,
          route: String(router.currentRoute.name ?? ''),
          target: appStore.getState().themeMode,
        })
        break
      case 'toggleHomeTools':
        appStore.setHomeAdvancedToolsVisible(!appStore.getState().homeAdvancedToolsVisible)
        break
      case 'jumpToSetting':
        void recordVisitorEvent({
          event: 'admin_entry_click',
          path: router.currentRoute.path,
          route: String(router.currentRoute.name ?? ''),
        })
        location.href = '/admin'
        break
    }
  }

  const sitename = st.sitename || 'Komari Monitor'

  return (
    <div
      className={`transition-all duration-200 top-0 sticky z-10 border-b border-transparent ${isScrolled ? '!border-slate-500/10 backdrop-blur-lg' : 'bg-transparent'}`}
    >
      <div className="px-4 flex-between h-14 max-w-[1280px] mx-auto">
        <div className="flex items-center gap-3 cursor-pointer" onClick={() => void router.push('/')}>
          <Avatar className="size-8">
            <AvatarImage src={siteFavicon} alt={sitename} />
            <AvatarFallback>{sitename.slice(0, 1)}</AvatarFallback>
          </Avatar>
          <h3 className="m-0 text-lg font-semibold">
            {sitename}
          </h3>
        </div>
        <TooltipProvider delayDuration={200}>
          <div className="flex items-center gap-2">
            <LanguageSwitcher />
            {buttons.map(button => (
              <Tooltip key={button.action}>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={button.title}
                    aria-pressed={button.pressed}
                    className={button.pressed ? 'bg-background/70 text-selection' : undefined}
                    onClick={() => handleButtonClick(button.action)}
                  >
                    <Icon icon={button.icon} width={18} height={18} />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{button.title}</TooltipContent>
              </Tooltip>
            ))}
          </div>
        </TooltipProvider>
      </div>
    </div>
  )
}
