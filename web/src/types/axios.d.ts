import 'axios'

declare module 'axios' {
  interface AxiosRequestConfig {
    _skipErrorHandler?: boolean
  }
}
