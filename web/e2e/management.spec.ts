import { expect, test, type APIRequestContext } from '@playwright/test'
import { copy, expectPage, fmt, login, prefix, token } from './support'

const auth = { Authorization: `Bearer ${token}` }
const PREFIX = 'e2e-pg-'

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
    await expect(page.getByText(fmt(copy.common.total, { count: 25 }))).toBeVisible()

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
  await expectPage(page, 'upstreams')
  // 失败时不能显示「暂无上游」，否则会被误读为没有配置
  await expect(page.getByText(copy.common.loadFailed)).toBeVisible()
  await expect(page.getByText(copy.pages.upstreams.emptyDescription)).toHaveCount(0)
  await expect(
    page.getByText(fmt(copy.feedback.serverError, { status: 500, detail: 'boom' })).first(),
  ).toBeVisible()

  // 恢复接口后点击重试，错误态消失
  await page.unroute('**/api/v1/config/upstreams')
  await page.getByTestId('load-error-retry').click()
  await expect(page.getByText(copy.common.loadFailed)).toHaveCount(0)
})

test('upstream page survives a network outage', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/upstreams', (route) => route.abort())
  await page.goto('/upstreams')
  await expectPage(page, 'upstreams')
  await expect(page.getByText(copy.common.loadFailed)).toBeVisible()
  await expect(page.getByText(copy.feedback.networkError).first()).toBeVisible()
})

test('geosite status failure shows an error state instead of a missing summary', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/geosite', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/geosite')
  await expectPage(page, 'geosite')
  await expect(page.getByText(copy.common.loadFailed)).toBeVisible()
  // 读不到状态时不能显示成「未找到」，否则会被当成数据未下载
  await expect(page.getByText(copy.pages.geosite.summary.missing)).toHaveCount(0)

  await page.unroute('**/api/v1/geosite')
  await page.getByTestId('load-error-retry').click()
  await expect(page.getByText(copy.common.loadFailed)).toHaveCount(0)
})

test('dashboard shows placeholders instead of a false outage when the status API fails', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/status', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/dashboard')

  // 读不到状态时提示接口不可用，而不是把「读不到」显示成「异常」
  await expect(page.getByText(copy.pages.dashboard.healthUnavailableTitle)).toBeVisible()
  const upCard = page.getByTestId('stat-upstreams')
  // 读不到状态时展示「—」而不是 0 个可用上游，另用提示说明接口不可用
  await expect(upCard).toContainText('—')
  await expect(page.getByText(copy.pages.dashboard.statusUnavailable)).toBeVisible()
  const ready = page.getByTestId('stat-ready')
  const runState = page.getByTestId('stat-status')
  await expect(ready).toContainText('—')
  await expect(ready).not.toContainText(copy.pages.dashboard.abnormal)
  await expect(runState).toContainText('—')
})

test('upstream delete shows a loading state and sends only one request', async ({ page, request }) => {
  const name = `${PREFIX}delete-slow`
  await request.delete(`/api/v1/config/upstreams/${name}`, { headers: auth })
  const created = await request.post('/api/v1/config/upstreams', {
    headers: auth,
    data: { name, url: 'http://127.0.0.1:9', types: ['http'] },
  })
  expect(created.ok()).toBeTruthy()

  await login(page)
  await page.goto('/upstreams')
  await expectPage(page, 'upstreams')

  // 拖慢 DELETE，观察按钮 loading 与并发保护
  let deletes = 0
  await page.route('**/api/v1/config/upstreams/*', async (route) => {
    if (route.request().method() !== 'DELETE') {
      await route.continue()
      return
    }
    deletes += 1
    await new Promise((resolve) => setTimeout(resolve, 1000))
    await route.continue()
  })

  const row = page.locator('.ant-table-tbody tr.ant-table-row').filter({ hasText: name })
  await row.getByTestId('row-delete').click()
  await page.locator('.ant-popover .ant-btn-primary').click()

  await expect(page.locator('.ant-btn-loading').first()).toBeVisible()
  await expect(row).toHaveCount(0, { timeout: 10000 })
  expect(deletes).toBe(1)
})

test('dashboard does not claim a fresh update when every API fails', async ({ page }) => {
  await login(page)

  for (const pattern of ['**/api/v1/status', '**/api/v1/traffic']) {
    await page.route(pattern, (route) =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
    )
  }
  await page.goto('/dashboard')

  await expect(page.getByText(copy.pages.dashboard.healthUnavailableTitle)).toBeVisible()
  // 全部接口失败时不能显示「最近更新」，否则等于宣称数据是新的
  await expect(page.getByText(prefix(copy.pages.dashboard.lastUpdated))).toHaveCount(0)
})

test('empty upstream list offers a create action', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/config/upstreams', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ upstreams: [] }) }),
  )
  await page.goto('/upstreams')

  await expect(page.locator('.ant-empty')).toContainText(copy.pages.upstreams.emptyDescription)
  await page.getByTestId('empty-create').click()
  await expect(page.getByRole('dialog')).toBeVisible()
})

test('empty rule list offers a create action', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/config/rules', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ rules: [] }) }),
  )
  await page.goto('/rules')

  await expect(page.locator('.ant-empty')).toContainText(copy.pages.rules.emptyDescription)
  await page.getByTestId('empty-create').click()
  await expect(page.getByRole('dialog')).toBeVisible()
})

test('traffic card offers retry instead of a settings link when loading fails', async ({ page }) => {
  await login(page)

  let calls = 0
  await page.route('**/api/v1/traffic', (route) => {
    calls += 1
    return route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' })
  })
  await page.goto('/dashboard')

  await expect(page.getByText(copy.pages.dashboard.trafficLoadFailed)).toBeVisible()
  await page.getByTestId('dashboard-traffic-card').getByTestId('traffic-retry').click()
  await expect.poll(() => calls).toBeGreaterThan(1)
})

test('proxy link modal points to basic settings when no address is detected', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ listen: '', proxy_endpoints: [] }),
    }),
  )
  await page.goto('/dashboard')
  await page.getByTestId('dashboard-proxy-link').click()

  await expect(page.getByText(copy.pages.dashboard.proxyModal.noTargets)).toBeVisible()
  await page
    .getByRole('dialog')
    .getByRole('button', { name: copy.pages.dashboard.proxyModal.goBasic })
    .click()
  await expect(page).toHaveURL(/\/settings\?section=basic/)
})

test('api doc search offers a clear action when nothing matches', async ({ page }) => {
  await login(page)
  await page.goto('/docs/api')

  await page.getByTestId('api-doc-search').fill('zzz-no-such-endpoint')
  await expect(
    page.getByText(fmt(copy.pages.apiDoc.emptyWithKeyword, { keyword: 'zzz-no-such-endpoint' })),
  ).toBeVisible()

  await page.getByTestId('api-doc-clear').click()
  await expect(page.getByText(prefix(copy.pages.apiDoc.emptyWithKeyword))).toHaveCount(0)
})

test('pprof page explains why the switch is disabled when the read fails', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/settings', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/pprof')

  await expect(page.getByText(copy.pages.pprof.aside.loadError)).toBeVisible()
  await expect(page.getByRole('switch')).toBeDisabled()
})

test('rendering errors surface a visible message instead of a blank page', async ({ page }) => {
  await login(page)

  // upstreams 不是数组：computed 里调用 .filter 会抛 TypeError，触发全局 errorHandler
  await page.route('**/api/v1/status', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ upstreams: 123, listen: '127.0.0.1:8890' }),
    }),
  )
  await page.goto('/dashboard')

  await expect(page.getByText(prefix(copy.feedback.renderError))).toBeVisible()
})

test('check-all names the upstreams that failed', async ({ page }) => {
  await login(page)

  const checkedAt = new Date().toISOString()
  await page.route('**/api/v1/config/upstreams/check', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        results: [
          { name: 'zh-check-ok', alive: true, checked_at: checkedAt },
          { name: 'zh-check-bad-1', alive: false, error: 'dial timeout', checked_at: checkedAt },
          { name: 'zh-check-bad-2', alive: false, error: 'dial timeout', checked_at: checkedAt },
        ],
      }),
    }),
  )
  await page.goto('/upstreams')
  await page.getByTestId('upstreams-check-all').click()

  // 只说 1/3 可用还得自己找是哪个，直接点名失败的
  await expect(
    page.getByText(
      fmt(copy.pages.upstreams.messages.partial, {
        alive: 1,
        total: 3,
        names: 'zh-check-bad-1',
        rest: '',
      }),
    ),
  ).toBeVisible()
  await expect(page.getByText(/zh-check-bad-1/)).toBeVisible()
  await expect(page.getByText(/zh-check-bad-2/)).toBeVisible()
})

test('toggling two upstreams keeps both rows loading', async ({ page, request }) => {
  const names = [`${PREFIX}toggle-a`, `${PREFIX}toggle-b`]
  for (const name of names) {
    await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, { headers: auth })
    const created = await request.post('/api/v1/config/upstreams', {
      headers: auth,
      data: { name, url: 'http://127.0.0.1:9', types: ['http'] },
    })
    expect(created.ok()).toBeTruthy()
  }

  await login(page)
  await page.goto('/upstreams')
  await expectPage(page, 'upstreams')

  // 拖慢 PUT，制造两行同时进行中的窗口
  await page.route('**/api/v1/config/upstreams/*', async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.continue()
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 1500))
    await route.continue()
  })

  const [rowA, rowB] = names.map((name) =>
    page.locator('.ant-table-tbody tr.ant-table-row').filter({ hasText: name }),
  )
  await rowA.locator('.ant-switch').click()
  await rowB.locator('.ant-switch').click()

  // 后点的行不能把先点那行的 loading 顶掉
  await expect(rowA.locator('.ant-switch-loading')).toBeVisible()
  await expect(rowB.locator('.ant-switch-loading')).toBeVisible()

  await expect(rowA.locator('.ant-switch-loading')).toHaveCount(0, { timeout: 10000 })
  await expect(rowB.locator('.ant-switch-loading')).toHaveCount(0, { timeout: 10000 })

  for (const name of names) {
    await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, { headers: auth })
  }
})

test('an out-of-range page falls back to the last page', async ({ page, request }) => {
  const names = [`${PREFIX}page-a`, `${PREFIX}page-b`]
  for (const name of names) {
    await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, { headers: auth })
    await request.post('/api/v1/config/upstreams', {
      headers: auth,
      data: { name, url: 'http://127.0.0.1:9', types: ['http'] },
    })
  }

  await login(page)
  await page.goto('/upstreams?page=99')

  // 越界页码会渲染空表格，看起来像上游被清空了
  await expect(page.locator('.ant-table-tbody tr.ant-table-row').first()).toBeVisible()
  await expect(page).toHaveURL(/page=1/)

  for (const name of names) {
    await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(name)}`, { headers: auth })
  }
})

test('empty user list offers a create action', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/config/users', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '{"users":[]}' }),
  )
  await page.goto('/users')

  await expect(page.locator('.ant-empty')).toContainText(copy.pages.users.emptyDescription)
  await page.getByTestId('empty-create').click()
  await expect(page.getByRole('dialog')).toBeVisible()
})

test('user page falls back a page after deleting the last row', async ({ page }) => {
  await login(page)

  let total = 21
  const users = () => ({
    users: Array.from({ length: total }, (_, i) => ({ username: `zh-user-${String(i + 1).padStart(2, '0')}` })),
  })
  await page.route('**/api/v1/config/users', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(users()) }),
  )
  await page.route('**/api/v1/config/users/*', (route) => {
    total = 20
    return route.fulfill({ status: 200, contentType: 'application/json', body: '{"status":"ok"}' })
  })

  await page.goto('/users')
  await page.locator('.ant-pagination-item-2').click()
  await expect(page.locator('.ant-table-tbody tr.ant-table-row')).toHaveCount(1)

  await page.getByTestId('row-delete').first().click()
  await page.locator('.ant-popover .ant-btn-primary').click()

  // 第 2 页删空后应回到第 1 页：20 条只剩一页，分页器随之隐藏
  await expect(page.locator('.ant-table-tbody tr.ant-table-row')).toHaveCount(20)
  await expect(page.locator('.ant-pagination')).toHaveCount(0)
})

test('traffic page falls back from an out-of-range page', async ({ page }) => {
  await login(page)

  const entries = Array.from({ length: 3 }, (_, i) => ({
    user: 'zh',
    upstream: 'zh-upstream',
    target: `t${i}.example.com`,
    bytes_in: 1,
    bytes_out: 1,
    total: 2,
  }))
  await page.route('**/api/v1/traffic', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: true, retention_days: 30, entries }),
    }),
  )
  await page.goto('/traffic?page=99')

  await expect(page.locator('.ant-table-tbody tr.ant-table-row').first()).toBeVisible()
  await expect(page).not.toHaveURL(/page=99/)
})

test('traffic disabled state links to the settings section', async ({ page }) => {
  await login(page)
  await page.route('**/api/v1/traffic', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ enabled: false, retention_days: 30, entries: [] }),
    }),
  )
  await page.goto('/traffic')

  // 未启用的事实由顶部 alert 说明，空态只报「暂无数据」，不再重复同一句话
  await expect(page.locator('.ant-empty')).toContainText(copy.pages.traffic.empty)
  await page.getByTestId('empty-action').click()
  await expect(page).toHaveURL(/\/settings\?section=quota/)
})

test('failing polling does not flood the page with error toasts', async ({ page }) => {
  await login(page)

  for (const pattern of ['**/api/v1/status', '**/api/v1/traffic']) {
    await page.route(pattern, (route) =>
      route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
    )
  }
  await page.goto('/dashboard')

  // 三个接口同时失败，同一句提示只该出现一次
  await page.waitForTimeout(1200)
  expect(await page.locator('.ant-message-notice').count()).toBeLessThanOrEqual(1)

  // 轮询静默：过一个轮询周期也不该再弹
  await page.waitForTimeout(10500)
  expect(await page.locator('.ant-message-notice').count()).toBeLessThanOrEqual(1)
})

test('about page can retry the version read', async ({ page }) => {
  await login(page)

  let calls = 0
  await page.route('**/api/v1/version', (route) => {
    calls += 1
    if (calls === 1) {
      return route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' })
    }
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ version: 'v9.9.9', commit: 'abc', build_time: 'now', go_version: 'go1.25', platform: 'linux' }),
    })
  })
  await page.goto('/about')

  await expect(page.getByText(copy.pages.about.loadError)).toBeVisible()
  await page.getByTestId('about-retry').click()
  await expect(page.getByText('v9.9.9')).toBeVisible()
  await expect(page.getByText(copy.pages.about.loadError)).toHaveCount(0)
})

test('moving a rule reveals where it landed', async ({ page, request }) => {
  const listRules = async () => {
    const resp = await request.get('/api/v1/config/rules', { headers: auth })
    return ((await resp.json()) as { rules?: { index: number; values: string[] }[] }).rules ?? []
  }
  const cleanup = async () => {
    for (const rule of await listRules()) {
      if (rule.values?.some((v) => v.startsWith('zh-move-'))) {
        await request.delete(`/api/v1/config/rules/${rule.index}`, { headers: auth })
      }
    }
  }

  await cleanup()
  for (const value of ['zh-move-1', 'zh-move-2', 'zh-move-3']) {
    const created = await request.post('/api/v1/config/rules', {
      headers: auth,
      data: { type: 'domain_suffix', values: [value], action: 'direct', upstreams: [] },
    })
    expect(created.ok()).toBeTruthy()
  }

  await login(page)
  await page.goto('/rules')

  const rows = page.locator('.ant-table-tbody tr.ant-table-row')
  await expect(rows).toHaveCount(3)

  await rows.filter({ hasText: 'zh-move-2' }).getByTestId('row-move-down').click()

  // 顺序变成 1、3、2，且落点行被高亮，长列表里不用自己找
  await expect(rows.nth(2)).toContainText('zh-move-2')
  await expect(page.locator('.ant-table-tbody tr.row-highlight')).toHaveCount(1)

  await cleanup()
})

test('api docs highlight the searched keyword', async ({ page }) => {
  await login(page)
  await page.goto('/docs/api')

  await page.getByTestId('api-doc-search').fill('健康检查') // copy-ok: 接口文档正文关键词

  // 描述里不标出命中位置，一屏十几条结果看不出为什么被匹配
  const mark = page.locator('.api-docs-detail mark').first()
  await expect(mark).toBeVisible()
  await expect(mark).toHaveText('健康检查') // copy-ok: 同上
})

test('bulk delete sends one batch request and keeps failed items selected', async ({ page }) => {
  await login(page)

  let batchCalls = 0
  let sent: string[] = []
  await page.route('**/api/v1/config/users/batch-delete', (route) => {
    batchCalls += 1
    sent = JSON.parse(route.request().postData() ?? '{}').usernames ?? []
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        status: 'ok',
        deleted: ['zh-bulk-1'],
        failed: [{ target: 'zh-bulk-2', error: 'user not found' }],
      }),
    })
  })
  await page.route('**/api/v1/config/users', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ users: [{ username: 'zh-bulk-1' }, { username: 'zh-bulk-2' }, { username: 'zh-bulk-3' }] }),
    }),
  )
  await page.goto('/users')

  const rows = page.locator('.ant-table-tbody tr.ant-table-row')
  await rows.filter({ hasText: 'zh-bulk-1' }).locator('.ant-checkbox').click()
  await rows.filter({ hasText: 'zh-bulk-2' }).locator('.ant-checkbox').click()

  await expect(page.locator('.bulk-bar')).toContainText(fmt(copy.common.selected, { count: 2 }))
  await page.getByTestId('users-bulk-delete').click()
  await page.locator('.ant-popconfirm-buttons .ant-btn-primary').click()

  await expect(page.locator('.ant-message')).toContainText(
    fmt(copy.pages.users.messages.deletedPartial, { deleted: 1, failed: 1, names: 'zh-bulk-2' }),
  )
  // 一个请求办完一批，不再逐条 DELETE
  expect(batchCalls).toBe(1)
  expect(sent.sort()).toEqual(['zh-bulk-1', 'zh-bulk-2'])
  // 失败项留在选中里，用户可以再点一次重试
  await expect(page.locator('.bulk-bar')).toContainText(fmt(copy.common.selected, { count: 1 }))
})

test('keyboard shortcuts focus search and save settings', async ({ page }) => {
  await login(page)

  await page.goto('/traffic')
  // 快捷键在 onMounted 里注册，等搜索框渲染出来再按，否则按键会落在挂载前
  const search = page.getByTestId('traffic-search')
  await expect(search).toBeVisible()
  await page.keyboard.press('/')
  await expect(search).toBeFocused()

  let saves = 0
  await page.route('**/api/v1/config/settings', async (route) => {
    if (route.request().method() !== 'PUT') return route.continue()
    saves += 1
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ success: true }),
    })
  })
  await page.goto('/settings')
  const interval = page.locator('#stg-check-interval-seconds')
  await interval.fill('77')
  await interval.press('Control+s')

  // 光标停在字段里也要能保存，这是 Ctrl+S 和按钮的主要差别
  await expect.poll(() => saves).toBe(1)
})
