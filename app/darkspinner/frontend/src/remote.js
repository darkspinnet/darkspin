export function remoteUsernameError(username) {
  const name = String(username || '').trim()
  if (!name) return 'Username is required.'
  if (!/^[A-Za-z0-9]+$/.test(name)) return 'Username can only contain letters and numbers.'
  if (name.length > 20) return 'Username is too long (maximum 20 characters).'
  return ''
}

export function remotePasswordError(password, isSavedPasswordAllowed = false) {
  if (!password && !isSavedPasswordAllowed) return 'Password is required.'
  return ''
}

// Go adds diagnostic breadcrumbs as errors cross operation boundaries. Show
// the human message in remote account forms while retaining raw fallback errors.
export function remoteErrorText(error) {
  const message = String(error?.message || error || '')
    .replace(/^(?:Error:\s*)+/, '')
    .replace(/^(?:[a-z][A-Za-z0-9]*(?:\[\d+\])?:\s*)+/, '')
  switch (message) {
    case 'invalid account or password': return 'Username or password is incorrect.'
    case 'identity is required': return 'Username is required.'
    case 'password is required':
    case 'password is required for remote registration':
    case 'password is required to delete a remote Crogenitor': return 'Password is required.'
    case 'there is already a registered user with that e-mail':
    case 'Username already exists on this server. Check your password or choose another username.': return 'Username already exists on this server, but the password is incorrect.'
    default: return message
  }
}
