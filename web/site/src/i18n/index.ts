import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import en from './en.json'
import zhCN from './zh-CN.json'
import zhTW from './zh-TW.json'
import ja from './ja.json'
import ru from './ru.json'
import ko from './ko.json'
import { readSiteLanguage, resolveSiteLanguage, siteLanguages, siteLanguageStorageKey, type SiteLanguage } from './language'

function browserLanguages(): readonly string[] {
  return navigator.languages?.length ? navigator.languages : [navigator.language]
}

function storedLanguage(): SiteLanguage | null {
  try { return readSiteLanguage(window.localStorage) } catch { return null }
}

// Do not cache automatic detection: only a deliberate selection takes precedence
// over the browser language on future visits. This key is separate from other SPAs.
let preference = storedLanguage()
void i18n.use(initReactI18next).init({
  resources: { en: { translation: en }, 'zh-CN': { translation: zhCN }, 'zh-TW': { translation: zhTW }, ja: { translation: ja }, ru: { translation: ru }, ko: { translation: ko } },
  lng: resolveSiteLanguage(preference, browserLanguages()),
  fallbackLng: 'en',
  supportedLngs: [...siteLanguages],
  initAsync: false,
  interpolation: { escapeValue: false },
})
const updateDocumentLanguage = () => { document.documentElement.lang = i18n.resolvedLanguage || 'en' }
updateDocumentLanguage()
i18n.on('languageChanged', updateDocumentLanguage)

export function selectSiteLanguage(language: SiteLanguage) {
  preference = language
  void i18n.changeLanguage(language)
  try { window.localStorage.setItem(siteLanguageStorageKey, language) } catch { /* Keep the current-page choice when storage is unavailable. */ }
}

export function subscribeSiteLanguage() {
  const storageChanged = (event: StorageEvent) => {
    if (event.key !== siteLanguageStorageKey && event.key !== null) return
    // A sessionStorage event must not overwrite this browser's saved preference.
    try { if (event.storageArea && event.storageArea !== window.localStorage) return } catch { return }
    preference = storedLanguage()
    void i18n.changeLanguage(resolveSiteLanguage(preference, browserLanguages()))
  }
  const systemChanged = () => { if (!preference) void i18n.changeLanguage(resolveSiteLanguage(null, browserLanguages())) }
  window.addEventListener('storage', storageChanged)
  window.addEventListener('languagechange', systemChanged)
  return () => {
    window.removeEventListener('storage', storageChanged)
    window.removeEventListener('languagechange', systemChanged)
  }
}

export default i18n
