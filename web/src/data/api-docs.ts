import type { ApiGroup } from './api-doc-groups/types'
import { coreGroups } from './api-doc-groups/core'
import { extraGroups } from './api-doc-groups/extra'

export type { ApiEndpoint, ApiExample, ApiGroup, ApiParam } from './api-doc-groups/types'

// 端点数据按业务分组维护，避免单文件持续膨胀；此处保持原有导出顺序不变。
export const apiGroups: ApiGroup[] = [...coreGroups, ...extraGroups]

export const authInfo = {
  method: 'Bearer Token',
  header: 'Authorization: Bearer <token>',
  configField: 'management_auth_token',
  description:
    '当 management_auth_token 配置为空时，所有管理接口允许匿名访问。配置非空时，所有请求必须携带 Authorization: Bearer <token> 头。管理端口绑定非回环地址时必须配置 management_auth_token。',
  bruteForceProtection: '单 IP 连续认证失败 10 次后封禁 5 分钟',
  unauthorizedResponse: {
    status: 401,
    headers: { 'WWW-Authenticate': 'Bearer realm="yigate-management"' },
    body: '{"status":"unauthorized"}',
  },
}
