import { Icon } from '@iconify/react'
import { language, t } from '@shared/i18n'
import { languages, saveLanguage } from '@shared/language'
import { buttonVariants } from '@shared/ui/button'

export default function LanguageSwitcher() {
  const title = `${t('language.label')}: ${languages.find(item => item.value === language)?.label}`

  function changeLanguage(value: string) {
    if (value === language) return
    if (!saveLanguage(value, selected => localStorage.setItem('i18nextLng', selected))) {
      window.$message?.error(t('language.saveError'))
      return
    }
    // Translated chart options and shared helpers are initialized on import.
    // Reload the current URL so every view uses the same language, preserving
    // the hash route and existing theme/group/favorite storage.
    window.location.reload()
  }

  return (
    <label title={title} className={`${buttonVariants({ variant: 'ghost', size: 'icon-sm' })} relative focus-within:ring-ring/50 focus-within:ring-[3px]`}>
      <Icon icon="tabler:language" width={18} height={18} aria-hidden="true" />
      <select
        aria-label={t('language.label')}
        value={language}
        onChange={event => changeLanguage(event.currentTarget.value)}
        className="absolute inset-0 size-full cursor-pointer opacity-0"
      >
        {languages.map(item => <option key={item.value} value={item.value}>{item.label}</option>)}
      </select>
    </label>
  )
}
