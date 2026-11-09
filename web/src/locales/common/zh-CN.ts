/** 通用文案：跨页面复用的按钮、状态词与全局提示。 */
export const common = {
  save: '保存',
  saving: '保存中',
  cancel: '取消',
  confirm: '确定',
  delete: '删除',
  deleting: '删除中',
  edit: '编辑',
  add: '新增',
  create: '创建',
  submit: '提交',
  submitting: '提交中',
  close: '关闭',
  refresh: '刷新',
  retry: '重试',
  search: '搜索',
  reset: '重置',
  copy: '复制',
  copied: '已复制',
  copyFailed: '复制失败',
  download: '下载',
  upload: '上传',
  detail: '详情',
  actions: '操作',
  status: '状态',
  check: '检查',
  enabled: '启用',
  disabled: '禁用',
  enable: '启用',
  disable: '禁用',
  all: '全部',
  yes: '是',
  no: '否',
  none: '无',
  unknown: '未知',
  loading: '加载中',
  empty: '暂无数据',
  selected: '已选 {count} 项',
  clearSelection: '取消选择',
  total: '共 {count} 条',
  seconds: '{count} 秒',
  minutes: '{count} 分钟',
  hours: '{count} 小时',
  days: '{count} 天',
  backHome: '返回仪表盘',
  requestFailed: '请求失败',
  requestErrorStatus: '请求错误 ({status})',
  loadFailed: '数据加载失败',
  loadFailedHint: '请检查后端服务是否运行，然后重试。',
  uptime: {
    days: '{days} 天 {hours} 小时 {minutes} 分钟',
    hours: '{hours} 小时 {minutes} 分钟',
    minutes: '{minutes} 分钟',
  },
}

/** 侧边栏导航与分组标题。 */
export const nav = {
  brand: 'YiGate',
  subtitle: '管理后台',
  dashboard: '仪表盘',
  manage: '管理',
  upstreams: '上游管理',
  rules: '路由规则',
  users: '用户管理',
  traffic: '流量审计',
  system: '系统',
  settings: '系统设置',
  geosite: 'GeoSite 数据',
  pprof: 'pprof 性能分析',
  help: '帮助',
  authDoc: '认证机制',
  apiDoc: 'API 文档',
  llmx: 'llmx.txt',
  about: '关于',
}

/** 顶栏、暗色模式与退出登录。 */
export const layout = {
  openNav: '打开导航菜单',
  toggleSidebar: '收起或展开侧栏',
  toLight: '切换到浅色模式',
  toDark: '切换到深色模式',
  logout: '退出',
  logoutConfirm: '确定要退出登录吗？',
  logoutOk: '退出',
  language: '语言',
}

export const login = {
  subtitle: '统一代理网关管理后台',
  tokenLabel: '管理 Token',
  tokenPlaceholder: '请输入 management_auth_token',
  submit: '登录',
  success: '登录成功',
  errorEmpty: '请输入管理 Token',
  errorInvalid: 'Token 验证失败，请检查 Token 或后端服务',
  hint: 'Token 对应配置文件中的 management_auth_token 字段。如果后端未设置 Token，可输入任意值登录。',
}

/** 浏览器标题，对应 router 的 name。 */
export const router = {
  login: '登录',
  dashboard: '仪表盘',
  upstreams: '上游管理',
  rules: '路由规则',
  geosite: 'GeoSite 数据',
  users: '用户管理',
  traffic: '流量审计',
  settings: '系统设置',
  pprof: 'pprof 性能分析',
  docsAuth: '认证文档',
  docsApi: 'API 文档',
  docsLlmx: 'llmx.txt',
  about: '关于',
  notFound: '页面不存在',
}

export const notFound = {
  title: '页面不存在',
  description: '地址可能已变更，或者你没有访问该页面的权限。',
}

/** 通用反馈文案：请求拦截器、错误边界等无处可归的提示。 */
export const feedback = {
  authFailed: '认证失败，请重新登录',
  forbidden: '无权限执行此操作',
  timeout: '请求超时',
  networkError: '网络错误，请检查后端服务是否运行',
  serverError: '服务器错误 ({status})：{detail}',
  renderError: '页面渲染出现异常，若持续出现请刷新页面',
}

export default { common, nav, layout, login, router, notFound, feedback }
