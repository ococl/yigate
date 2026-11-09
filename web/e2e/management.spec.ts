import { expect, test, type APIRequestContext, type Page } from '@playwright/test'

const token = process.env.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'
const auth = { Authorization: `Bearer ${token}` }
const PREFIX = 'e2e-pg-'

async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByPlaceholder('请输入 management_auth_token').fill(token)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/dashboard$/)
}

async function cleanUpstreams(request: APIRequestContext): Promise<void> {
  const resp = await request.get('/api/v1/config/upstreams', { headers: auth })
  if (!resp.ok()) return
  const body = (await resp.json()) as { upstreams?: { name: string }[] }
  for (const up of body.upstreams ?? []) {
    if (up.name.startsWith(PREFIX)) {
      await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(up.name)}`, { headers: auth })
    }
  }
}

test('upstream list paginates, keeps page in the URL and resets on page-size change', async ({
  page,
  request,
}) => {
  await cleanUpstreams(request)
  const created: string[] = []
  try {
    for (let i = 1; i <= 25; i++) {
      const name = `${PREFIX}${String(i).padStart(2, '0')}`
      const resp = await request.post('/api/v1/config/upstreams', {
        headers: auth,
        data: { name, url: `http://127.0.0.1:${20000 + i}`, types: ['http'], priority: 1, weight: 1 },
      })
      expect(resp.ok(), `create ${name}`).toBeTruthy()
      created.push(name)
    }

    await login(page)
    await page.goto('/upstreams')
    const rows = page.locator('.ant-table-tbody tr.ant-table-row')
    await expect(rows).toHaveCount(10)
    await expect(page.getByText('共 25 条')).toBeVisible()

    await page.locator('.ant-pagination-item-2').click()
    await expect(page).toHaveURL(/[?&]page=2/)
    await expect(rows).toHaveCount(10)

    await page.reload()
    await expect(page.locator('.ant-pagination-item-active')).toHaveText('2')
    await expect(rows).toHaveCount(10)

    // 切换每页条数后回到第一页，避免停留在越界页码只显示零星几条
    await page.locator('.ant-pagination-options .ant-select').click()
    await page.locator('.ant-select-dropdown .ant-select-item-option').filter({ hasText: '20' }).click()
    await expect(page).toHaveURL(/[?&]page=1/)
    await expect(page).toHaveURL(/[?&]size=20/)
    await expect(rows).toHaveCount(20)
  } finally {
    for (const name of created) {
      await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, { headers: auth })
    }
  }
})

test('upstream list shows a retryable error state instead of an empty list when the API fails', async ({
  page,
}) => {
  await login(page)

  await page.route('**/api/v1/config/upstreams', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/upstreams')
  await expect(page.getByRole('heading', { name: '上游管理' })).toBeVisible()
  // 失败时不能显示「暂无上游」，否则会被误读为没有配置
  await expect(page.getByText('数据加载失败')).toBeVisible()
  await expect(page.getByText('暂无上游，请先新增一个上游代理')).toHaveCount(0)
  await expect(page.getByText('服务器错误').first()).toBeVisible()

  // 恢复接口后点击重试，错误态消失
  await page.unroute('**/api/v1/config/upstreams')
  await page.locator('.ant-result-extra .ant-btn').click()
  await expect(page.getByText('数据加载失败')).toHaveCount(0)
})

test('upstream page survives a network outage', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/upstreams', (route) => route.abort())
  await page.goto('/upstreams')
  await expect(page.getByRole('heading', { name: '上游管理' })).toBeVisible()
  await expect(page.getByText('数据加载失败')).toBeVisible()
  await expect(page.getByText('网络错误').first()).toBeVisible()
})

test('dashboard warns instead of reporting a false outage when the health API fails', async ({ page }) => {
  await login(page)

  // 健康接口以站点根路径请求（/health），不是 /api/v1 前缀；登录本身也依赖它，所以登录后再拦截
  await page.route(/\/health$/, (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/dashboard')

  await expect(page.getByText('无法获取网关运行状态')).toBeVisible()
  const ready = page.locator('.ant-statistic').filter({ hasText: '路由 / 管理就绪' })
  const runState = page.locator('.ant-statistic').filter({ hasText: '运行状态' })
  // 读不到健康状态时展示「—」，而不是误报「异常」
  await expect(ready).toContainText('—')
  await expect(ready).not.toContainText('异常')
  await expect(runState).toContainText('—')
})

test('geosite status failure shows an error state instead of "未找到"', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/geosite', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/geosite')
  await expect(page.getByRole('heading', { name: 'GeoSite 数据' })).toBeVisible()
  await expect(page.getByText('数据加载失败')).toBeVisible()
  // 读不到状态时不能显示成「未找到」，否则会被当成数据未下载
  await expect(page.getByText('未找到')).toHaveCount(0)

  await page.unroute('**/api/v1/geosite')
  await page.locator('.ant-result-extra .ant-btn').click()
  await expect(page.getByText('数据加载失败')).toHaveCount(0)
})
