export default {
  title: '认证机制',
  subtitle: 'YiGate 管理接口认证说明',
  navTitle: '文档目录',
  sections: {
    overview: '认证概览',
    security: '安全防护',
    usage: '使用示例',
    config: '配置说明',
  },
  overview: {
    method: '认证方式',
    header: '请求头',
    configField: '配置字段',
    description: '说明',
  },
  security: {
    bruteForce: '暴力破解防护',
    unauthorizedStatus: '未认证响应状态码',
    authenticateHeader: 'WWW-Authenticate 响应头',
    unauthorizedBody: '未认证响应体',
  },
  usage: {
    withToken: '携带认证令牌的请求',
    withoutToken: '未携带令牌的请求（token 非空时）',
    responseComment: '# 响应:',
  },
  config: {
    forceTitle: '强制认证场景',
    forceBefore: '管理端口绑定非回环地址时，',
    forceMid: '必须配置。启用 pprof 且管理端口非回环时同样必填。',
    yamlTitle: 'YAML 配置示例',
    envBefore: '配置中的 ',
    envMid: '会被环境变量展开。环境变量前缀为 ',
    envAfter: '。缺失或空值会导致启动失败。',
    rotateTitle: '令牌轮换',
    rotateBefore: '令牌可在「系统设置 → 管理面板与安全」中修改，或通过 ',
    rotateMid: ' 传入 ',
    rotateAfter:
      '。修改后立即生效且无需重启，旧令牌当场失效，当前会话会被强制重新登录。清除令牌（传空字符串）仅在管理端口绑定回环地址时允许。',
  },
}
