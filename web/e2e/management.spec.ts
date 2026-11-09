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

test('upstream page stays usable and shows an error when the API fails', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/upstreams', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/upstreams')
  await expect(page.getByRole('heading', { name: '上游管理' })).toBeVisible()
  await expect(page.getByText(/失败|错误|异常/).first()).toBeVisible()

  // 断网（请求直接失败）时同样给出提示而不是白屏
  await page.unroute('**/api/v1/config/upstreams')
  await page.route('**/api/v1/config/upstreams', (route) => route.abort())
  await page.reload()
  await expect(page.getByRole('heading', { name: '上游管理' })).toBeVisible()
  await expect(page.getByText(/失败|错误|异常|网络/).first()).toBeVisible()
})
