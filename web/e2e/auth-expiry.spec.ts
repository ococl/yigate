import { expect, test } from '@playwright/test'
import { expectPage, login, navItem } from './support'

// 管理令牌过期后，API 层必须清理本地凭据并让路由回到登录页，
// 而不是保留一个看似已登录但实际所有请求都 401 的界面。
test('expired token clears the session and returns to login', async ({ page }) => {
  await login(page, '/dashboard')

  await page.route('**/api/v1/**', (route) =>
    route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'token expired' }),
    }),
  )

  // 切页会触发新的管理 API 请求，从而触发 401 拦截器。
  await navItem(page, 'upstreams').click()

  await expect(page).toHaveURL(/\/login$/)
  await expectPage(page, 'login')
  await expect(page.evaluate(() => localStorage.getItem('yigate_token'))).resolves.toBeNull()
})
