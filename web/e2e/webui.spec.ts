import { expect, test } from '@playwright/test'
import { copy, expectPage, login, navItem } from './support'

test('unauthenticated dashboard redirects to login', async ({ page }) => {
  await page.goto('/dashboard')

  await expect(page).toHaveURL(/\/login$/)
  await expectPage(page, 'login')
})

test('rejects an invalid management token', async ({ page }) => {
  await page.goto('/login')
  await page.getByTestId('login-token').fill('invalid-token')
  await page.getByTestId('login-submit').click()

  await expect(page.getByText(copy.login.errorInvalid)).toBeVisible()
  await expect(page).toHaveURL(/\/login$/)
})

test('logs in and renders every management page', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))

  await page.goto('/dashboard')
  await login(page)

  const routes = [
    ['/upstreams', 'upstreams'],
    ['/rules', 'rules'],
    ['/users', 'users'],
    ['/traffic', 'traffic'],
    ['/settings', 'settings'],
    ['/geosite', 'geosite'],
    ['/pprof', 'pprof'],
    ['/docs/auth', 'auth-doc'],
    ['/docs/api', 'api-doc'],
    ['/docs/llms', 'llms'],
    ['/about', 'about'],
  ] as const

  for (const [path, anchor] of routes) {
    await page.goto(path)
    await expectPage(page, anchor)
  }

  expect(pageErrors).toEqual([])
})

test('unknown route renders a 404 page instead of blank layout', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/definitely-not-exist')

  await expect(page.getByText('404', { exact: true })).toBeVisible()
  await expect(page.getByText(copy.pages.notFound.title)).toBeVisible()
  await expect(page.getByTestId('not-found-home')).toBeVisible()
})

test('traffic search is persisted in the URL', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/traffic')
  const search = page.getByTestId('traffic-search')
  await search.fill('127.0.0.1')
  await expect(page).toHaveURL(/[?&]q=127\.0\.0\.1/)

  await page.reload()
  await expect(search).toHaveValue('127.0.0.1')
})

test('settings warns before leaving with unsaved changes', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/settings')
  const save = page.getByTestId('settings-save')
  await expect(save).toBeDisabled()

  await page.getByTestId('settings-section-quota').click()
  const auditSwitch = page.locator('.settings-detail').getByRole('switch').first()
  const initial = await auditSwitch.getAttribute('aria-checked')
  await auditSwitch.click()
  await expect(save).toBeEnabled()

  // 离开时出现确认框，取消后停留在设置页
  await navItem(page, 'dashboard').click()
  const confirm = page.getByRole('dialog')
  await expect(confirm).toBeVisible()
  await expect(confirm.locator('.ant-modal-title')).toHaveText(copy.pages.settings.leave.title)
  await confirm.getByRole('button', { name: copy.pages.settings.leave.cancel }).click()
  await expect(page).toHaveURL(/\/settings/)

  // 还原开关后回到无修改状态，此时应可直接离开且保存按钮再次禁用
  if ((await auditSwitch.getAttribute('aria-checked')) !== initial) {
    await auditSwitch.click()
  }
  await expect(save).toBeDisabled()
  await navItem(page, 'dashboard').click()
  await expect(page).toHaveURL(/\/dashboard$/)
})

test('document title follows the current page', async ({ page }) => {
  await page.goto('/login')
  await expect(page).toHaveTitle(`${copy.router.login} · YiGate`)

  await login(page)
  await expect(page).toHaveTitle(`${copy.router.dashboard} · YiGate`)

  await navItem(page, 'rules').click()
  await expect(page).toHaveTitle(`${copy.router.rules} · YiGate`)
})

test('llms.txt is served for AI clients and mirrored in the help page', async ({ page, request }) => {
  // AI 客户端直接取原文：匿名可读，Content-Type 要能正确解码中文。
  // 正文由后端生成，不在前端词条里，这里按内容抽样断言。
  const resp = await request.get('/llms.txt')
  expect(resp.status()).toBe(200)
  expect(resp.headers()['content-type']).toContain('text/plain')
  const raw = await resp.text()
  expect(raw).toContain('# YiGate')
  expect(raw).toContain('先读后写') // copy-ok: 后端生成的文档正文

  await page.goto('/dashboard')
  await login(page)
  await page.goto('/docs/llms')
  await expectPage(page, 'llms')
  await expect(page.locator('.llms-text')).toContainText('先读后写') // copy-ok: 同上
})
