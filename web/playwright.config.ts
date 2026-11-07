import { defineConfig } from '@playwright/test'

const baseURL = process.env.YIGATE_E2E_BASE_URL ?? 'http://127.0.0.1:8894'

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  reporter: process.env.CI
    ? [['list'], ['html', { open: 'never' }]]
    : [['list']],
  use: {
    baseURL,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'bash ../scripts/run-webui-e2e.sh',
    url: new URL('/login', baseURL).toString(),
    reuseExistingServer: !process.env.CI,
    timeout: 90_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
})
