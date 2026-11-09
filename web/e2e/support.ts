import { expect, type Locator, type Page } from '@playwright/test'
import zh from '../src/locales/zh-CN'

/**
 * E2E 的定位与断言约定：
 * - 找元素一律用 data-testid：文案、语言、组件模板写法变化都不影响用例；
 * - 需要断言文案时引用 `copy`（词条即唯一来源），不要再抄一份中文字面量。
 */
export const copy = zh

// Playwright 在 Node 里执行，但 e2e 目录也进 vue-tsc 的类型检查，这里不依赖 @types/node。
const env = (globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env

export const token = env?.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'

/** 把词条模板里的 {name} 换成实际值，例如「共 {count} 条」。 */
export function fmt(template: string, vars: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (placeholder, key: string) =>
    key in vars ? String(vars[key]) : placeholder,
  )
}

/** 词条模板里第一个占位符之前的固定文案，用于「不应出现」这类前缀断言。 */
export function prefix(template: string): string {
  return template.split('{')[0].trim()
}

/** 页面锚点：每个视图根节点都带 page-<route> 的 testid。 */
export function pageAnchor(page: Page, name: string): Locator {
  return page.getByTestId(`page-${name}`)
}

export async function expectPage(page: Page, name: string): Promise<void> {
  await expect(pageAnchor(page, name)).toBeVisible()
}

/** 侧栏与移动端抽屉共用同一组导航 testid。 */
export function navItem(page: Page, name: string): Locator {
  return page.getByTestId(`nav-${name}`)
}

export async function login(page: Page, path = '/login'): Promise<void> {
  await page.goto(path)
  await page.getByTestId('login-token').fill(token)
  await page.getByTestId('login-submit').click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await expectPage(page, 'dashboard')
}

/** 管理页通用入口：先登录，再直达目标页面并确认页面已渲染。 */
export async function openPage(page: Page, path: string, anchor: string): Promise<void> {
  await page.goto(path)
  await expectPage(page, anchor)
}
