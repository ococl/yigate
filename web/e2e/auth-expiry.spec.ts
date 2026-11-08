import { expect, test } from '@playwright/test'

const token = process.env.YIGATE_E2E_TOKEN ?? 'yigate-webui-e2e-token'

// 管理令牌过期后，API 层必须清理本地凭据并让路由回到登录页，
// 而不是保留一个看似已登录但实际所有请求都 401 的界面。
test('expired token clears the session and returns to login', async ({ page }) => {
  await page.goto('/dashboard')
  await page.getByPlaceholder('请输入 management_auth_token').fill(token)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/dashboard$/)

  await page.route('**/api/v1/**', (route) => route.fulfill({
    status: 401,
    contentType: 'application/json',
    body: JSON.stringify({ error: 'token expired' }),
  }))

  // 切页会触发新的管理 API 请求，从而触发 401 拦截器。
  await page.getByRole('link', { name: '上游管理' }).click()

  await expect(page).toHaveURL(/\/login$/)
  await expect(page.evaluate(() => localStorage.getItem('yigate_token'))).resolves.toBeNull()
})
