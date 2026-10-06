// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { getImageUrl } from './image'

const SITE_ICON_LINK_ID = 'site-favicon'
const SITE_TOUCH_ICON_LINK_ID = 'site-apple-touch-icon'

export function resolveSiteIconHref(value: unknown, size: 32 | 180 = 32): string {
  const icon = String(value || '').trim()
  // 图标内容始终由服务端当前品牌配置决定，v 仅用于更新浏览器缓存。
  const version = icon ? `&v=${encodeURIComponent(icon)}` : ''
  return getImageUrl(`/api/v1/public/site-icon?size=${size}${version}`)
}

export function applySiteIcon(value: unknown) {
  const icons = [
    { id: SITE_ICON_LINK_ID, rel: 'icon', size: 32 as const },
    { id: SITE_TOUCH_ICON_LINK_ID, rel: 'apple-touch-icon', size: 180 as const },
  ]
  for (const icon of icons) {
    let link = document.getElementById(icon.id) as HTMLLinkElement | null
    if (!link) {
      link = document.createElement('link')
      link.id = icon.id
      document.head.appendChild(link)
    }
    link.rel = icon.rel
    link.type = 'image/png'
    link.sizes.value = `${icon.size}x${icon.size}`
    link.href = resolveSiteIconHref(value, icon.size)
  }
}
