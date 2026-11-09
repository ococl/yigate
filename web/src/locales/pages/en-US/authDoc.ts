export default {
  title: 'Authentication',
  subtitle: 'How YiGate authenticates management API requests',
  navTitle: 'Contents',
  sections: {
    overview: 'Overview',
    security: 'Protections',
    usage: 'Examples',
    config: 'Configuration',
  },
  overview: {
    method: 'Method',
    header: 'Request header',
    configField: 'Config field',
    description: 'Notes',
  },
  security: {
    bruteForce: 'Brute-force protection',
    unauthorizedStatus: 'Unauthorized status',
    authenticateHeader: 'WWW-Authenticate header',
    unauthorizedBody: 'Unauthorized body',
  },
  usage: {
    withToken: 'Request with a token',
    withoutToken: 'Request without a token (when the token is set)',
    responseComment: '# response:',
  },
  config: {
    forceTitle: 'When authentication is mandatory',
    forceBefore: 'When the management port binds a non-loopback address, ',
    forceMid: ' is required. The same applies when pprof is enabled on a non-loopback port.',
    yamlTitle: 'YAML example',
    envBefore: 'In the config, ',
    envMid: ' is expanded from the environment. The prefix is ',
    envAfter: '. A missing or empty value fails startup.',
    rotateTitle: 'Token rotation',
    rotateBefore: 'Change the token under Settings → Panel & security, or send ',
    rotateMid: ' to ',
    rotateAfter:
      '. It takes effect immediately without a restart; the old token stops working at once and the current session must sign in again. Clearing the token (empty string) is only allowed when the management port binds loopback.',
  },
}
