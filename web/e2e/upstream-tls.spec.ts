import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test'
import { copy, login, token } from './support'

const auth = { Authorization: `Bearer ${token}` }
const PREFIX = 'e2e-tls-'

interface UpstreamItem {
  name: string
  url: string
  types: string[]
  tls_protocol?: string
  tls_ca_file?: string
  tls_server_name?: string
}

interface UpstreamListBody {
  upstreams: UpstreamItem[]
}

async function listUpstreams(request: APIRequestContext): Promise<UpstreamItem[]> {
  const resp = await request.get('/api/v1/config/upstreams', { headers: auth })
  expect(resp.ok(), 'list upstreams').toBeTruthy()
  const body = (await resp.json()) as UpstreamListBody
  return body.upstreams ?? []
}

/** 删除本用例创建的上游，避免与分页/其它用例互相干扰。 */
async function cleanUpstreams(request: APIRequestContext): Promise<void> {
  for (const up of await listUpstreams(request)) {
    if (up.name.startsWith(PREFIX)) {
      await request.delete(`/api/v1/config/upstreams/${encodeURIComponent(up.name)}`, { headers: auth })
    }
  }
}

async function findUpstream(request: APIRequestContext, name: string): Promise<UpstreamItem | undefined> {
  return (await listUpstreams(request)).find((up) => up.name === name)
}

async function openAddDialog(page: Page): Promise<Locator> {
  await page.getByTestId('upstreams-add').click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()
  return dialog
}

async function rowFor(page: Page, name: string): Promise<Locator> {
  return page.locator('.ant-table-tbody tr.ant-table-row').filter({ hasText: name })
}

/** CA 字段是 AutoComplete：testid 落在根节点上，实际输入框在内部。 */
function certCaInput(dialog: Locator): Locator {
  return dialog.locator('[data-testid="up-tls-ca"] input, input[data-testid="up-tls-ca"]')
}

/** 选择传输协议；选项文案来自词条，避免在用例里抄一份中文。 */
async function selectProtocol(page: Page, dialog: Locator, label: string): Promise<void> {
  await dialog.getByTestId('up-tls-protocol').click()
  await page.locator('.ant-select-item-option').filter({ hasText: label }).click()
}

test('upstream form persists national crypto fields and keeps them on partial edit', async ({
  page,
  request,
}) => {
  await cleanUpstreams(request)
  const name = `${PREFIX}gm`
  try {
    await login(page)
    await page.goto('/upstreams')

    const dialog = await openAddDialog(page)
    await dialog.locator('#up-name').fill(name)
    await dialog.locator('#up-url').fill('https://127.0.0.1:18443')
    // 默认勾选 socks5，改为 https 才能配置传输层协议。
    await dialog.getByRole('checkbox', { name: 'socks5', exact: true }).uncheck()
    await dialog.getByRole('checkbox', { name: 'https', exact: true }).check()
    await selectProtocol(page, dialog, copy.pages.upstreams.tlsProtocols.gm)
    // 证书路径支持从配置目录补全：点开下拉直接选，避免手输路径写错。
    const caInput = certCaInput(dialog)
    await caInput.click()
    const caOption = page.locator('.ant-select-item-option').filter({ hasText: 'certs/gm/ca.crt' })
    await expect(caOption).toBeVisible()
    await caOption.click()
    await expect(caInput).toHaveValue('certs/gm/ca.crt')
    await dialog.getByTestId('up-tls-sni').fill('gw.example.com')
    await dialog.getByRole('button', { name: copy.pages.upstreams.form.okAdd }).click()
    await expect(dialog).toBeHidden()

    // 落库结果以 API 为准，避免只验证了表单回显。
    const created = await findUpstream(request, name)
    expect(created?.tls_protocol, 'tls_protocol persisted').toBe('gm')
    expect(created?.tls_ca_file, 'tls_ca_file persisted').toBe('certs/gm/ca.crt')
    expect(created?.tls_server_name, 'tls_server_name persisted').toBe('gw.example.com')

    // 编辑弹窗必须回填国密字段，否则一次保存就会把配置抹掉。
    await (await rowFor(page, name)).getByTestId('row-edit').click()
    await expect(dialog).toBeVisible()
    await expect(dialog.getByTestId('up-tls-protocol')).toContainText(copy.pages.upstreams.tlsProtocols.gm)
    await expect(certCaInput(dialog)).toHaveValue('certs/gm/ca.crt')
    await expect(dialog.getByTestId('up-tls-sni')).toHaveValue('gw.example.com')

    // 清空 CA 是显式操作，必须真的清掉；协议保持不变。
    await certCaInput(dialog).fill('')
    await dialog.getByRole('button', { name: copy.pages.upstreams.form.okUpdate }).click()
    await expect(dialog).toBeHidden()

    const updated = await findUpstream(request, name)
    expect(updated?.tls_ca_file, 'tls_ca_file cleared').toBeFalsy()
    expect(updated?.tls_protocol, 'tls_protocol kept').toBe('gm')
    expect(updated?.tls_server_name, 'tls_server_name kept').toBe('gw.example.com')
  } finally {
    await cleanUpstreams(request)
  }
})

test('forcing gm on a non-https upstream is rejected in the form', async ({ page, request }) => {
  await cleanUpstreams(request)
  const name = `${PREFIX}bad`
  try {
    await login(page)
    await page.goto('/upstreams')

    const dialog = await openAddDialog(page)
    await dialog.locator('#up-name').fill(name)
    await dialog.locator('#up-url').fill('socks5://127.0.0.1:1080')
    await selectProtocol(page, dialog, copy.pages.upstreams.tlsProtocols.gm)
    await dialog.getByRole('button', { name: copy.pages.upstreams.form.okAdd }).click()

    // 前端拦截：弹窗不关闭，URL 下方给出明确原因，且不应发出创建请求。
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText(copy.pages.upstreams.messages.tlsProtocolRequiresHttps)).toBeVisible()
    expect(await findUpstream(request, name)).toBeUndefined()
  } finally {
    await cleanUpstreams(request)
  }
})
