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
    ['/about', '关于'],
  ] as const

  for (const [path, heading] of routes) {
    await page.goto(path)
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
  }

  expect(pageErrors).toEqual([])
})
