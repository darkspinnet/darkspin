export function panelError(failure) {
  const message = String(failure)
  const errors = [
    ['diagnostic_invalid_request', 'Choose a valid diagnostic file and try again.'],
    ['diagnostic_not_found', 'The file is no longer available. Refresh the list.'],
    ['diagnostic_unavailable', 'The server could not access the diagnostic file. Try again later.'],
    ['diagnostic_conflict', 'A diagnostic operation is in progress or the file changed. Refresh and try again.'],
    ['diagnostic_too_large', 'The diagnostic exceeds the supported size or file-count limit.'],
    ['update_unavailable', 'Update information is unavailable. Refresh or sign in again; no update status is assumed.'],
    ['update_unsupported', 'This deployment does not support update checks.'],
    ['update_check_unsupported', 'This deployment does not support update checks.'],
    ['unauthorized', 'The password is incorrect or your control panel session expired.'],
    ['forbidden', 'This account cannot access the control panel.'],
    ['version_mismatch', 'The launcher and server need compatible major and minor versions.'],
    ['rate_limited', 'Too many requests. Wait before trying again.'],
    ['unavailable', 'The server cannot read account information right now. Try again later.'],
    ['target_online', 'This account must be offline before making this change.'],
    ['name_in_use', 'That display name is already in use.'],
    ['invalid_password', 'The current password is incorrect.'],
    ['invalid_name', 'Enter a valid display name.'],
    ['username_required', 'Enter a display name containing 1 to 20 letters and numbers.'],
    ['username_pattern', 'Display names may contain only letters and numbers.'],
    ['username_too_long', 'Display names must contain 1 to 20 letters and numbers.'],
    ['password_required', 'Enter a password.'],
    ['password_too_long', 'Passwords must be at most 72 UTF-8 bytes.'],
    ['outcome_unknown', 'The request was accepted, but its outcome is unknown. Refresh administration history before trying again.'],
    ['maintenance_conflict', 'Another maintenance operation is active. Refresh its status and cancel it before choosing a different action.'],
    ['maintenance_stopping', 'Session cleanup has begun and cannot be cancelled yet.'],
    ['maintenance_unsupported', 'This deployment does not support the requested maintenance action.'],
    ['invalid_request', 'Check the supplied account details and try again.'],
    ['not_found', 'This account or operation is no longer available.'],
    ['admin_protected', 'This administrator account cannot be moderated.'],
    ['conflict', 'The account changed while this request was being processed. Refresh and try again.'],
  ]
  return errors.find(([code]) => message.includes(`controlPanel:${code}`))?.[1] || 'Unable to complete the control panel request. Check your connection and sign in again if needed.'
}

export function maintenancePhaseLabel(phase) {
  const phases = {
    preparing:'PREPARING', awaiting_activation:'AWAITING ACTIVATION', warning:'WARNING', draining:'DRAINING', held:'HELD',
    stopping:'STOPPING', restarting:'RESTARTING', updating:'UPDATING', starting:'STARTING', validating:'VALIDATING',
    healthy:'HEALTHY', completed:'COMPLETED', rolled_back:'ROLLED BACK', failed:'FAILED', cancelled:'CANCELLED',
  }
  return phases[phase] || 'UNKNOWN'
}

export function maintenanceActionLabel(action) {
  return { hold:'HOLD IN MAINTENANCE', restart:'RESTART', update:'UPDATE WITH BACKUP' }[action] || 'UNKNOWN'
}
