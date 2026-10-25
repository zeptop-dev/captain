import en from './i18n/en.json'
import zhCN from './i18n/zh-CN.json'
import zhTW from './i18n/zh-TW.json'
import ja from './i18n/ja.json'
import ko from './i18n/ko.json'
import ru from './i18n/ru.json'
import { readLanguage } from './language'
const resources: Record<string, Record<string, string>> = {en,'zh-CN':zhCN,'zh-TW':zhTW,ja,ko,ru}
export const language = readLanguage(() => localStorage.getItem('i18nextLng'), navigator.language)
document.documentElement.lang = language
export function t(source: string): string { return resources[language]?.[source] ?? source }
