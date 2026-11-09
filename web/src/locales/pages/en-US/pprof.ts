export default {
  title: 'pprof profiling',
  subtitle: 'Go runtime profiling endpoints served on the management port, for diagnosing CPU, memory and blocking issues.',
  enabledTag: 'Enabled',
  disabledTag: 'Disabled',
  aside: {
    cardTitle: 'Collection switch',
    switchLabel: 'pprof debug endpoints',
    switchTooltip: 'When on, /debug/pprof/ is reachable on the management port',
    switchOn: 'On',
    switchOff: 'Off',
    loadError: 'Failed to read the switch state',
    saveHint: 'Applies immediately after saving',
    warningTitle: 'pprof is a sensitive debug surface',
    warningDescription:
      'Only enable it when the management port is bound to loopback or a trusted network; never expose it publicly. Profiling adds extra runtime overhead.',
    usageTitle: 'Usage',
    usageDesc: 'Assuming the local management port is 8890, download a capture and analyze it with',
    usageDescTail: '.',
    cpuTitle: 'Capture a CPU profile (30s)',
    analyzeTitle: 'Analyze locally',
    tokenDesc: 'If a management token is set, include',
    tokenDescTail: ' in the request; browsers opening the link directly must authenticate with the panel first.',
  },
  notEnabled: {
    title: 'pprof is not enabled',
    description: 'Turn on the switch on the left to make the endpoints below reachable.',
  },
  open: 'Open',
  groups: {
    overview: 'Overview',
    cpu: 'CPU & tracing',
    memory: 'Memory',
    concurrency: 'Concurrency & blocking',
    runtime: 'Runtime',
  },
  endpoints: {
    index: {
      label: 'pprof index',
      description: 'All available runtime profiles and a goroutine overview',
    },
    profile: {
      label: 'CPU profile',
      description: 'Sample 30 seconds of CPU usage; good for finding hot code paths',
    },
    trace: {
      label: 'Execution trace',
      description: 'Capture 5 seconds of runtime events; good for scheduler and blocking analysis',
    },
    heap: {
      label: 'Heap',
      description: 'Snapshot of current heap allocations to find retained memory',
    },
    allocs: {
      label: 'Allocations',
      description: 'Cumulative allocation samples to find allocation-heavy code',
    },
    goroutine: {
      label: 'Goroutines',
      description: 'Stacks of all goroutines to spot leaks and deadlocks',
    },
    block: {
      label: 'Block profile',
      description: 'Blocking events caused by synchronization primitives',
    },
    mutex: {
      label: 'Mutex profile',
      description: 'Mutex contention',
    },
    threadcreate: {
      label: 'Thread creation',
      description: 'OS thread creation activity',
    },
  },
  messages: {
    enabled: 'pprof debug endpoints enabled',
    disabled: 'pprof debug endpoints disabled',
  },
}
