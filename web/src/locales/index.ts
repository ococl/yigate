import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import enUS from './en-US'

export const SUPPORTED_LOCALES = ['zh-CN', 'en-US'] as const
export type AppLocale = (typeof SUPPORTED_LOCALES)[number]

const LOCALE_KEY = 'yigate_locale'

/** 语言列表，用于切换菜单展示；label 用各自语言书写，不跟随当前语言翻译。 */
export const LOCALE_OPTIONS: { value: AppLocale; label: string }[] = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en-US', label: 'English' },
]

export function isAppLocale(value: unknown): value is AppLocale {
  return value === 'zh-CN' || value === 'en-US'
}

/** 读取已保存的语言；没有记录时固定用简体中文，避免首次访问被系统语言改变界面。 */
export function readStoredLocale(): AppLocale {
  const saved = localStorage.getItem(LOCALE_KEY)
  return isAppLocale(saved) ? saved : 'zh-CN'
}

export const i18n = createI18n({
  legacy: false,
  locale: readStoredLocale(),
  fallbackLocale: 'zh-CN',
  messages: {
    'zh-CN': zhCN,
    'en-US': enUS,
  },
})

function syncDocumentLang(locale: AppLocale) {
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('lang', locale)
  }
}

/** 切换语言并持久化，同时同步 <html lang> 供浏览器与辅助技术识别。 */
export function setLocale(locale: AppLocale) {
  i18n.global.locale.value = locale
  localStorage.setItem(LOCALE_KEY, locale)
  syncDocumentLang(locale)
}

syncDocumentLang(readStoredLocale())

export default i18n
