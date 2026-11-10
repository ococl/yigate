export default {
  title: 'Users',
  subtitle: 'Proxy authentication users. After a password change, new connections use the new credentials.',
  addUser: 'Add user',
  authNotice: {
    message:
      'Proxy authentication stays off while no user exists. Once any user is added, every proxy connection must supply credentials.',
    description:
      'With multiple listeners, adding or removing users applies to all of them. Disabled users cannot authenticate; disabling every user is the same as anonymous access.',
  },
  emptyDescription: 'No users. An empty list means proxy authentication is off.',
  columns: {
    username: 'Username',
    status: 'Status',
    actions: 'Actions',
  },
  status: {
    enabled: 'Enabled',
    disabled: 'Disabled',
  },
  anonymousRisk: {
    title: 'Fall back to anonymous access?',
    description:
      'This is the last enabled proxy user. Removing or disabling it means the port requires no authentication at all, so anyone on the network can proxy through it.',
    detail:
      'The system will write allow_anonymous_proxy: true so the change takes effect. Add another user first if you still need authentication.',
    confirm: 'Continue anyway',
  },
  changePassword: 'Change password',
  deleteConfirm: 'The user will no longer authenticate through the proxy. Delete anyway?',
  bulkDelete: 'Delete selected',
  bulkDeleteConfirm: 'Delete the {count} selected users? This cannot be undone.',
  form: {
    addTitle: 'Add user',
    username: 'Username',
    usernamePlaceholder: 'Username',
    password: 'Password',
    passwordPlaceholder: 'Password',
    okAdd: 'Add',
    resetTitle: 'Change password: {name}',
    newPassword: 'New password',
    newPasswordPlaceholder: 'At least 4 characters',
    okSave: 'Save',
  },
  messages: {
    usernameRequired: 'Enter a username',
    passwordRequired: 'Enter a password',
    passwordTooShort: 'Password must be at least 4 characters',
    added: 'User added',
    passwordUpdated: 'Password updated for {name}',
    deleted: 'User deleted',
    deletedCount: 'Deleted {count} users',
    deletedPartial: 'Deleted {deleted}, {failed} failed: {names}',
    enabled: 'User {name} enabled',
    disabled: 'User {name} disabled',
    listSeparator: ', ',
  },
}
