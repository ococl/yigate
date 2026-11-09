export interface ApiParam {
  name: string
  type: string
  required: boolean
  description: string
}

export interface ApiExample {
  request?: string
  response: string
}

export interface ApiEndpoint {
  id: string
  method: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string
  summary: string
  description: string
  auth: boolean
  authNote?: string
  params: ApiParam[]
  requestBody?: string
  responseBody: string
  example: ApiExample
  statusCodes: { code: number; description: string }[]
}

export interface ApiGroup {
  key: string
  title: string
  description: string
  endpoints: ApiEndpoint[]
}
