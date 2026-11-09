import { expect, test, type Page } from '@playwright/test'

const token = process.env.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'

test.use({ viewport: { width: 390, height: 844 } })

async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByPlaceholder('请输入 management_auth_token').fill(token)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/dashboard$/)
}

test('mobile layout uses drawer navigation and keeps content usable', async ({ page }) => {
  await login(page)

  // 窄屏不再渲染固定侧栏，内容区占满视口宽度
  await expect(page.locator('.app-sider')).toHaveCount(0)
  const contentWidth = await page.locator('.content').evaluate((el) => el.getBoundingClientRect().width)
  expect(contentWidth).toBeGreaterThan(340)

  const viewportOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  )
  expect(viewportOverflow).toBeLessThanOrEqual(1)

  // 顶部菜单按钮打开抽屉，选择菜单后自动关闭
  await page.getByRole('button', { name: '打开导航菜单' }).click()
  const drawer = page.getByRole('dialog')
  await expect(drawer).toBeVisible()
  await drawer.getByText('上游管理').click()
  await expect(page).toHaveURL(/\/upstreams$/)
  await expect(page.getByRole('dialog')).toBeHidden()
  await expect(page.getByRole('heading', { name: '上游管理' })).toBeVisible()
})

test('every management page fits the mobile viewport', async ({ page }) => {
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
    // 页面本身不应横向滚动；宽表格/代码块应在容器内部滚动。
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    )
    expect(overflow, `${path} 出现横向滚动`).toBeLessThanOrEqual(1)
  }
})

test('settings switches to a section picker on mobile', async ({ page }) => {
  await login(page)
  await page.goto('/settings')

  const picker = page.getByRole('combobox', { name: '设置分类' })
  await expect(picker).toBeVisible()
  await expect(page.locator('.settings-aside')).toBeHidden()

  await picker.click()
  await page
    .locator('.ant-select-dropdown:visible .ant-select-item-option')
    .filter({ hasText: '流量审计与配额' })
    .click()
  await expect(page.getByRole('heading', { name: '流量审计与配额' })).toBeVisible()
})

test('mobile empty list keeps the create action reachable', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/config/upstreams', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ upstreams: [] }) }),
  )
  await page.goto('/upstreams')

  const empty = page.locator('.ant-empty').filter({ hasText: '暂无上游' })
  await expect(empty).toBeVisible()
  await empty.getByRole('button', { name: '新增上游' }).click()

  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  // 窄屏下弹窗不应超出视口
  const box = await dialog.boundingBox()
  expect(box?.width ?? 0).toBeLessThanOrEqual(390)
})

test('mobile user list paginates instead of rendering every card', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/users', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        users: Array.from({ length: 21 }, (_, i) => ({ username: `zh-mobile-${i + 1}` })),
      }),
    }),
  )
  await page.goto('/users')

  await expect(page.locator('.mobile-card')).toHaveCount(20)
  await page.locator('.mobile-pagination .ant-pagination-item-2').click()
  await expect(page.locator('.mobile-card')).toHaveCount(1)
})
