/** 后端管理接口的错误响应体（字段名与 main/proxy 的 JSON 输出一致）。 */
export interface ApiErrorBody {
  error?: string
  status?: string
  code?: string
}

/** 统一的 API 错误类型，调用方可据此区分鉴权、权限、服务端与网络错误。 */
export class ApiError extends Error {
  readonly status: number
  readonly code?: string
  readonly detail: string

  constructor(status: number, body?: ApiErrorBody | null, fallback?: string) {
    const detail = body?.error || body?.status || fallback || `请求错误 (${status})`
    super(detail)
    this.name = 'ApiError'
    this.status = status
    this.code = body?.code
    this.detail = detail
  }
}

/** 管理接口成功时直接返回数据体，这里仅作为显式类型别名便于后续演进。 */
export type ApiResponse<T> = T

/**
 * 从 axios/普通异常中提取结构化 API 错误。
 * 非 HTTP 错误（超时、DNS、主动取消）返回 null，由调用方按网络错误处理。
 */
export function parseApiError(error: unknown): ApiError | null {
  if (error instanceof ApiError) return error
  if (!error || typeof error !== 'object') return null
  const response = (error as { response?: { status?: number; data?: unknown } }).response
  if (!response || typeof response.status !== 'number') return null

  let body: ApiErrorBody | undefined
  if (response.data && typeof response.data === 'object') {
    const raw = response.data as Record<string, unknown>
    body = {
      error: typeof raw.error === 'string' ? raw.error : undefined,
      status: typeof raw.status === 'string' ? raw.status : undefined,
      code: typeof raw.code === 'string' ? raw.code : undefined,
    }
  }
  return new ApiError(response.status, body)
}
