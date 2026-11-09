import { expect, test, type Page } from '@playwright/test'
import { copy, expectPage, login } from './support'

/** 打开指定章节，并等待设置加载完成，避免默认值被服务端值覆盖导致脏状态回退。 */
async function openSection(page: Page, section: string): Promise<void> {
  const loaded = page.waitForResponse(
    (r) => r.url().includes('/api/v1/config/settings') && r.request().method() === 'GET',
  )
  await page.goto(`/settings?section=${section}`)
  await loaded
  await expectPage(page, 'settings')
}

async function pickDnsType(page: Page, value: string): Promise<void> {
  await page.locator('#stg-dns-type').click()
  await page.locator('.ant-select-item-option').filter({ hasText: value }).first().click()
  await page.waitForTimeout(200)
}

const fieldErrors = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll<HTMLElement>('.ant-form-item [class*="explain"]')]
      .map((el) => el.innerText.trim())
      .filter(Boolean),
  )

test('settings validates DoH fields inline and jumps to the section', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'doh')
  await page.locator('#stg-doh-url').fill('')
  await page.getByTestId('settings-save').click()

  await expect(page.getByText(copy.pages.settings.validation.dohRequired).first()).toBeVisible()
  await expect(page).toHaveURL(/section=dns/)
  await expect(page.getByText(copy.pages.settings.messages.saved)).toHaveCount(0)

  await page.locator('#stg-doh-url').fill('ftp://dns.example/dns-query')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.validation.dohInvalid).first()).toBeVisible()

  // 修改后内联错误自动清除（toast 可能仍在，只看字段区域）
  await page.locator('#stg-doh-url').fill('https://dns.example/dns-query')
  await expect(page.locator('.ant-form-item [class*="explain"]')).toHaveCount(0)

  await pickDnsType(page, 'dot')
  await page.locator('#stg-dot-addr').fill('1.1.1.1')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.validation.dotInvalid).first()).toBeVisible()
})

test('settings jumps to the section holding the first invalid field', async ({ page }) => {
  await login(page)

  await openSection(page, 'destination')
  await page.locator('#stg-deny-ports-text').fill('25, abc')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.validation.portsInvalid.split('：')[0]).first()).toBeVisible()
  await expect(page).toHaveURL(/section=destination/)

  await openSection(page, 'panel')
  await page.locator('#stg-token-input').fill('short')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.validation.tokenTooShort).first()).toBeVisible()
  await expect(page).toHaveURL(/section=panel/)
})

test('settings saves once the invalid fields are corrected', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'doh')
  await page.locator('#stg-doh-url').fill('https://dns.example/dns-query')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.messages.saved)).toBeVisible()

  // 还原为 system，避免影响其它用例
  await openSection(page, 'dns')
  await pickDnsType(page, 'system')
  await page.getByTestId('settings-save').click()
  await expect(page.getByText(copy.pages.settings.messages.saved)).toBeVisible()
})

test('settings shows inline errors only after save, not while typing', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'dot')
  await page.locator('#stg-dot-addr').fill('1.1.1.1')
  expect(await fieldErrors(page)).toEqual([])
})

test('settings reports a load failure instead of showing an empty form', async ({ page }) => {
  await login(page)

  await page.route('**/api/v1/config/settings', (route) =>
    route.fulfill({ status: 500, contentType: 'application/json', body: '{"error":"boom","status":"error"}' }),
  )
  await page.goto('/settings')

  // 读不到配置时不能摆出一份空表单，更不能允许保存覆盖服务端配置
  await expect(page.getByText(copy.pages.settings.loadError.loadTitle)).toBeVisible()
  await expect(page.getByText(copy.pages.settings.loadError.loadDescription)).toBeVisible()
  await expect(page.getByTestId('settings-save')).toBeDisabled()
  await expect(page.locator('.settings-body')).toHaveCount(0)

  await page.getByTestId('settings-retry').click()
  await expect(page.getByText(copy.pages.settings.loadError.loadTitle)).toBeVisible()
})

test('retention 0 stays "use default" instead of becoming one day', async ({ page }) => {
  await login(page)

  let sent: Record<string, unknown> | null = null
  await page.route('**/api/v1/config/settings', (route) => {
    if (route.request().method() === 'PUT') {
      sent = route.request().postDataJSON()
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{"status":"ok"}' })
    }
    return route.continue()
  })

  await page.goto('/settings?section=quota')
  await page.locator('#stg-traffic-retention-days').fill('0')
  await page.getByTestId('settings-save').click()

  // 0 的语义是「使用默认 30 天」，被强转成 1 会让流量明细一天后就被清掉
  await expect.poll(() => sent?.traffic_retention_days).toBe(0)
})
