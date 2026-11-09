import { expect, test, type Page } from '@playwright/test'

const token = process.env.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'

async function login(page: Page): Promise<void> {
  await page.getByPlaceholder('请输入 management_auth_token').fill(token)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/dashboard$/)
  await expect(page.getByRole('heading', { name: '仪表盘' })).toBeVisible()
}

test('unauthenticated dashboard redirects to login', async ({ page }) => {
  await page.goto('/dashboard')

  await expect(page).toHaveURL(/\/login$/)
  await expect(page.getByRole('heading', { name: 'YiGate' })).toBeVisible()
})

test('rejects an invalid management token', async ({ page }) => {
  await page.goto('/login')
  await page.getByPlaceholder('请输入 management_auth_token').fill('invalid-token')
  await page.getByRole('button', { name: '登录', exact: true }).click()

  await expect(page.getByText('Token 验证失败，请检查 Token 或后端服务')).toBeVisible()
  await expect(page).toHaveURL(/\/login$/)
})

test('logs in and renders every management page', async ({ page }) => {
  const pageErrors: string[] = []
  page.on('pageerror', (error) => pageErrors.push(error.message))

  await page.goto('/dashboard')
  await login(page)

  const routes = [
    ['/upstreams', '上游管理'],
    ['/rules', '路由规则'],
    ['/users', '用户管理'],
    ['/traffic', '流量审计'],
    ['/settings', '系统设置'],
    ['/geosite', 'GeoSite 数据'],
    ['/pprof', 'pprof 性能分析'],
    ['/docs/auth', '认证机制'],
    ['/docs/api', 'API 文档'],
    ['/docs/llmx', 'llmx.txt'],
    ['/about', '关于'],
  ] as const

  for (const [path, heading] of routes) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  }

  expect(pageErrors).toEqual([])
})

test('unknown route renders a 404 page instead of blank layout', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/definitely-not-exist')

  await expect(page.getByText('404', { exact: true })).toBeVisible()
  await expect(page.getByText('页面不存在或已被移除')).toBeVisible()
  await expect(page.getByRole('button', { name: '返回仪表盘' })).toBeVisible()
})

test('traffic search is persisted in the URL', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/traffic')
  await page.getByPlaceholder('搜索用户、上游或目标').fill('127.0.0.1')
  await expect(page).toHaveURL(/[?&]q=127\.0\.0\.1/)

  await page.reload()
  await expect(page.getByPlaceholder('搜索用户、上游或目标')).toHaveValue('127.0.0.1')
})

test('settings warns before leaving with unsaved changes', async ({ page }) => {
  await page.goto('/dashboard')
  await login(page)

  await page.goto('/settings')
  const save = page.getByRole('button', { name: '保存设置' })
  await expect(save).toBeDisabled()

  await page.locator('.settings-aside').getByText('流量审计与配额').click()
  const auditSwitch = page.locator('.settings-detail').getByRole('switch').first()
  const initial = await auditSwitch.getAttribute('aria-checked')
  await auditSwitch.click()
  await expect(save).toBeEnabled()

  // 离开时出现确认框，取消后停留在设置页
  await page.locator('.app-sider').getByText('仪表盘').click()
  const confirm = page.getByRole('dialog')
  await expect(confirm).toBeVisible()
  await expect(confirm.locator('.ant-modal-title')).toHaveText('有未保存的设置')
  await confirm.getByRole('button', { name: '继续编辑' }).click()
  await expect(page).toHaveURL(/\/settings/)

  // 还原开关后回到无修改状态，此时应可直接离开且保存按钮再次禁用
  if ((await auditSwitch.getAttribute('aria-checked')) !== initial) {
    await auditSwitch.click()
  }
  await expect(save).toBeDisabled()
  await page.locator('.app-sider').getByText('仪表盘').click()
  await expect(page).toHaveURL(/\/dashboard$/)
})

test('document title follows the current page', async ({ page }) => {
  await page.goto('/login')
  await expect(page).toHaveTitle('登录 · YiGate')

  await login(page)
  await expect(page).toHaveTitle('仪表盘 · YiGate')

  await page.getByRole('link', { name: '路由规则' }).click()
  await expect(page).toHaveTitle('路由规则 · YiGate')
})

test('llmx.txt is served for AI clients and mirrored in the help page', async ({ page, request }) => {
  // AI 客户端直接取原文：匿名可读，Content-Type 要能正确解码中文
  const resp = await request.get('/llmx.txt')
  expect(resp.status()).toBe(200)
  expect(resp.headers()['content-type']).toContain('text/plain')
  const raw = await resp.text()
  expect(raw).toContain('# YiGate')
  expect(raw).toContain('先读后写')

  await page.goto('/dashboard')
  await login(page)
  await page.goto('/docs/llmx')
  await expect(page.locator('.llmx-text')).toContainText('先读后写')
})
