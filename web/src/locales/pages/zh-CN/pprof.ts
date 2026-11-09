export default {
  title: 'pprof 性能分析',
  subtitle: '通过管理端口访问 Go 运行时性能采集端点，用于排查 CPU、内存与阻塞问题。',
  enabledTag: '已启用',
  disabledTag: '未启用',
  aside: {
    cardTitle: '采集开关',
    switchLabel: 'pprof 调试端点',
    switchTooltip: '启用后可在管理端口访问 /debug/pprof/',
    switchOn: '启用',
    switchOff: '关闭',
    loadError: '读取开关状态失败',
    saveHint: '保存后立即生效',
    warningTitle: 'pprof 属于敏感调试入口',
    warningDescription: '仅在管理端口绑定回环地址或受信网络时启用，请勿直接暴露到公网。采集期间会带来额外性能开销。',
    usageTitle: '使用方式',
    usageDesc: '以本机管理端口 8890 为例，采集结果下载后可用',
    usageDescTail: '分析。',
    cpuTitle: '采集 CPU Profile（30 秒）',
    analyzeTitle: '本地分析',
    tokenDesc: '若管理令牌已启用，请在请求中携带',
    tokenDescTail: '；浏览器直接打开链接时需先通过管理端认证。',
  },
  notEnabled: {
    title: 'pprof 尚未启用',
    description: '开启左侧开关后，下方入口才可访问。',
  },
  open: '打开',
  groups: {
    overview: '概览',
    cpu: 'CPU 与执行追踪',
    memory: '内存',
    concurrency: '并发与阻塞',
    runtime: '运行时',
  },
  endpoints: {
    index: {
      label: 'pprof 首页',
      description: '查看所有可用的运行时 profile 与当前 goroutine 概览',
    },
    profile: {
      label: 'CPU Profile',
      description: '采集 30 秒 CPU 使用情况，适合定位计算热点',
    },
    trace: {
      label: '执行追踪',
      description: '采集 5 秒运行时事件，适合分析调度与阻塞',
    },
    heap: {
      label: '堆内存',
      description: '当前堆内存分配快照，定位内存驻留',
    },
    allocs: {
      label: '内存分配',
      description: '累计内存分配采样，定位频繁分配',
    },
    goroutine: {
      label: 'Goroutine',
      description: '全部 goroutine 调用栈，排查泄漏与卡死',
    },
    block: {
      label: '阻塞分析',
      description: '同步原语导致的阻塞事件',
    },
    mutex: {
      label: '互斥锁',
      description: '互斥锁竞争情况',
    },
    threadcreate: {
      label: '线程创建',
      description: '操作系统线程创建情况',
    },
  },
  messages: {
    enabled: 'pprof 调试端点已启用',
    disabled: 'pprof 调试端点已关闭',
  },
}
