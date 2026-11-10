/** Shared copy: buttons, status words and global feedback reused across pages. */
export const common = {
  save: 'Save',
  saving: 'Saving',
  cancel: 'Cancel',
  confirm: 'Confirm',
  delete: 'Delete',
  deleting: 'Deleting',
  edit: 'Edit',
  add: 'Add',
  create: 'Create',
  submit: 'Submit',
  submitting: 'Submitting',
  close: 'Close',
  refresh: 'Refresh',
  retry: 'Retry',
  search: 'Search',
  reset: 'Reset',
  copy: 'Copy',
  copied: 'Copied',
  copyFailed: 'Copy failed',
  download: 'Download',
  upload: 'Upload',
  detail: 'Details',
  actions: 'Actions',
  status: 'Status',
  check: 'Check',
  enabled: 'Enabled',
  disabled: 'Disabled',
  enable: 'Enable',
  disable: 'Disable',
  all: 'All',
  yes: 'Yes',
  no: 'No',
  none: 'None',
  unknown: 'Unknown',
  loading: 'Loading',
  empty: 'No data',
  selected: '{count} selected',
  clearSelection: 'Clear selection',
  total: '{count} in total',
  seconds: '{count}s',
  minutes: '{count} min',
  hours: '{count} h',
  days: '{count} d',
  backHome: 'Back to dashboard',
  requestFailed: 'Request failed',
  requestErrorStatus: 'Request failed ({status})',
  loadFailed: 'Failed to load data',
  loadFailedHint: 'Check that the backend is running, then retry.',
  uptime: {
    days: '{days}d {hours}h {minutes}m',
    hours: '{hours}h {minutes}m',
    minutes: '{minutes}m',
  },
}

/** Sidebar navigation and group titles. */
export const nav = {
  brand: 'YiGate',
  dashboard: 'Dashboard',
  manage: 'Manage',
  upstreams: 'Upstreams',
  rules: 'Routing rules',
  users: 'Users',
  traffic: 'Traffic audit',
  system: 'System',
  settings: 'Settings',
  geosite: 'GeoSite data',
  pprof: 'pprof profiling',
  help: 'Help',
  authDoc: 'Authentication',
  apiDoc: 'API reference',
  llms: 'llms.txt',
  about: 'About',
}

/** Header toolbar, theme switch and sign-out. */
export const layout = {
  openNav: 'Open navigation',
  toggleSidebar: 'Collapse or expand sidebar',
  toLight: 'Switch to light mode',
  toDark: 'Switch to dark mode',
  logout: 'Sign out',
  logoutConfirm: 'Sign out of the console?',
  logoutOk: 'Sign out',
  language: 'Language',
}

export const login = {
  subtitle: 'Unified proxy gateway console',
  tokenLabel: 'Management token',
  tokenPlaceholder: 'Enter management_auth_token',
  submit: 'Sign in',
  success: 'Signed in',
  errorEmpty: 'Enter the management token',
  errorInvalid: 'Token rejected. Check the token or the backend service.',
  hint: 'The token maps to management_auth_token in the config file. If no token is configured, any value works.',
}

/** Browser titles keyed by route name. */
export const router = {
  login: 'Sign in',
  dashboard: 'Dashboard',
  upstreams: 'Upstreams',
  rules: 'Routing rules',
  geosite: 'GeoSite data',
  users: 'Users',
  traffic: 'Traffic audit',
  settings: 'Settings',
  pprof: 'pprof profiling',
  docsAuth: 'Authentication guide',
  docsApi: 'API reference',
  docsLlms: 'llms.txt',
  about: 'About',
  notFound: 'Page not found',
}

export const notFound = {
  title: 'Page not found',
  description: 'The address may have changed, or you do not have access to this page.',
}

/** Global feedback used by the request interceptor and the error boundary. */
export const feedback = {
  authFailed: 'Authentication failed, please sign in again',
  forbidden: 'You are not allowed to perform this action',
  timeout: 'Request timed out',
  networkError: 'Network error. Check whether the backend is running.',
  serverError: 'Server error ({status}): {detail}',
  renderError: 'The page failed to render. Refresh if it keeps happening.',
}

export default { common, nav, layout, login, router, notFound, feedback }
