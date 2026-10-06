// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

export function httpContactUrl(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return ''
  try {
    const url = new URL(value.trim())
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return ''
    return url.href
  } catch {
    return ''
  }
}

export function twitterProfileUrl(value: unknown): string {
  const href = httpContactUrl(value)
  if (!href) return ''
  const url = new URL(href)
  if (!['x.com', 'www.x.com', 'twitter.com', 'www.twitter.com'].includes(url.hostname) || url.port) return ''
  const match = url.pathname.match(/^\/([A-Za-z0-9_]{1,15})\/?$/)
  if (!match?.[1]) return ''
  const reserved = ['home', 'explore', 'search', 'notifications', 'messages', 'settings', 'i', 'intent', 'login', 'logout', 'signup', 'compose', 'tos', 'privacy']
  if (reserved.includes(match[1].toLowerCase())) return ''
  return `https://${url.hostname}/${match[1]}`
}
