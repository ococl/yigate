import { expect, test, type Page } from '@playwright/test'

const token = process.env.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'

async function login(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByPlaceholder('请输入 management_auth_token').fill(token)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/dashboard$/)
}

/** 打开指定章节，并等待设置加载完成，避免默认值被服务端值覆盖导致脏状态回退。 */
async function openSection(page: Page, section: string): Promise<void> {
  const loaded = page.waitForResponse(
    (r) => r.url().includes('/api/v1/config/settings') && r.request().method() === 'GET',
  )
  await page.goto(`/settings?section=${section}`)
  await loaded
  await expect(page.getByRole('heading', { name: '系统设置' })).toBeVisible()
}

async function pickDnsType(page: Page, value: string): Promise<void> {
  await page.locator('#stg-dns-type').click()
  await page.locator('.ant-select-item-option').filter({ hasText: value }).first().click()
  await page.waitForTimeout(200)
}

const fieldErrors = (page: Page) =>
  page.evaluate(() =>
    [...document.querySelectorAll('.ant-form-item [class*="explain"]')]
      .map((el) => el.innerText.trim())
      .filter(Boolean),
  )

test('settings validates DoH fields inline and jumps to the section', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'doh')
  await page.locator('#stg-doh-url').fill('')
  await page.getByRole('button', { name: '保存设置' }).click()

  await expect(page.getByText('请填写 DoH URL').first()).toBeVisible()
  await expect(page).toHaveURL(/section=dns/)
  await expect(page.getByText('设置已保存')).toHaveCount(0)

  await page.locator('#stg-doh-url').fill('ftp://dns.example/dns-query')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText('DoH URL 需以 http:// 或 https:// 开头').first()).toBeVisible()

  // 修改后内联错误自动清除（toast 可能仍在，只看字段区域）
  await page.locator('#stg-doh-url').fill('https://dns.example/dns-query')
  await expect(page.locator('.ant-form-item [class*="explain"]')).toHaveCount(0)

  await pickDnsType(page, 'dot')
  await page.locator('#stg-dot-addr').fill('1.1.1.1')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText(/DoT 地址格式为 host:port/).first()).toBeVisible()
})

test('settings jumps to the section holding the first invalid field', async ({ page }) => {
  await login(page)

  await openSection(page, 'destination')
  await page.locator('#stg-deny-ports-text').fill('25, abc')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText(/端口需为 1-65535 的整数/).first()).toBeVisible()
  await expect(page).toHaveURL(/section=destination/)

  await openSection(page, 'panel')
  await page.locator('#stg-token-input').fill('short')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText(/管理令牌至少 8 位/).first()).toBeVisible()
  await expect(page).toHaveURL(/section=panel/)
})

test('settings saves once the invalid fields are corrected', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'doh')
  await page.locator('#stg-doh-url').fill('https://dns.example/dns-query')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText('设置已保存')).toBeVisible()

  // 还原为 system，避免影响其它用例
  await openSection(page, 'dns')
  await pickDnsType(page, 'system')
  await page.getByRole('button', { name: '保存设置' }).click()
  await expect(page.getByText('设置已保存')).toBeVisible()
})

test('settings shows inline errors only after save, not while typing', async ({ page }) => {
  await login(page)
  await openSection(page, 'dns')

  await pickDnsType(page, 'dot')
  await page.locator('#stg-dot-addr').fill('1.1.1.1')
  expect(await fieldErrors(page)).toEqual([])
})
