import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import zh from './zh-CN.json'
import zhTW from './zh-TW.json'
import en from './en.json'
import ja from './ja.json'
import ru from './ru.json'
import ko from './ko.json'

i18n.use(LanguageDetector).use(initReactI18next).init({
  resources: { 'zh-CN': { translation: zh }, 'zh-TW': { translation: zhTW }, en: { translation: en }, ja: { translation: ja }, ru: { translation: ru }, ko: { translation: ko } },
  fallbackLng: 'zh-CN', supportedLngs: ['zh-CN', 'zh-TW', 'en', 'ja', 'ru', 'ko'], interpolation: { escapeValue: false },
  detection: { order: ['localStorage', 'navigator'], caches: ['localStorage'] },
})
export default i18n
