import { IconLanguage } from '@tabler/icons-react'
import { useTranslation } from 'react-i18next'
import { selectSiteLanguage } from '../i18n'
import { parseSiteLanguage, siteLanguages } from '../i18n/language'

const names = { en: 'English', 'zh-CN': '简体中文', 'zh-TW': '繁體中文', ja: '日本語', ru: 'Русский', ko: '한국어' }

export function SiteLanguageSelector() {
  const { t, i18n } = useTranslation()
  return <label className="site-language-selector">
    <IconLanguage size={18} aria-hidden />
    <select aria-label={t('common.language')} value={i18n.resolvedLanguage || 'en'} onChange={event => {
      const language = parseSiteLanguage(event.target.value)
      if (language) selectSiteLanguage(language)
    }}>
      {siteLanguages.map(language => <option key={language} value={language} lang={language}>{names[language]}</option>)}
    </select>
  </label>
}
