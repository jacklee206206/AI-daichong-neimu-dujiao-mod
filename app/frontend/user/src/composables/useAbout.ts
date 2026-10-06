// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '../stores/app'
import { usePageSeo } from './usePageSeo'
import type { PublicContactConfig } from '../api/types'
import { httpContactUrl, twitterProfileUrl } from '../utils/contactLinks'

/**
 * 关于页共享逻辑（classic + vault 双模板共用）。
 * 完整保留原 views/About.vue 的行为，仅抽离为 composable。
 */
export function useAbout() {
  const { t, locale } = useI18n()
  const appStore = useAppStore()

  usePageSeo({
    title: () => t('nav.about'),
    canonicalPath: () => '/about',
  })

  const aboutConfig = computed(() => appStore.config?.about || null)
  const contactConfig = computed<PublicContactConfig | null>(() => appStore.config?.contact || null)
  const twitterUrl = computed(() => twitterProfileUrl(contactConfig.value?.twitter))
  const whatsappUrl = computed(() => httpContactUrl(contactConfig.value?.whatsapp))
  const twitterLabel = computed(() => t('footer.twitter'))

  const resolveLocalizedText = (raw: unknown): string => {
    if (!raw || typeof raw !== 'object') {
      return ''
    }

    const record = raw as Record<string, unknown>
    const lang = String(locale.value || appStore.locale || 'zh-CN')
    const candidates = [record[lang], record['zh-CN'], record['zh-TW'], record['en-US']]

    for (const candidate of candidates) {
      if (typeof candidate === 'string' && candidate.trim() !== '') {
        return candidate.trim()
      }
    }

    return ''
  }

  const heroTitle = computed(() => resolveLocalizedText(aboutConfig.value?.hero?.title))
  const heroSubtitle = computed(() => resolveLocalizedText(aboutConfig.value?.hero?.subtitle))
  const introductionText = computed(() => resolveLocalizedText(aboutConfig.value?.introduction))
  const servicesTitle = computed(() => resolveLocalizedText(aboutConfig.value?.services?.title))
  const contactTitle = computed(() => resolveLocalizedText(aboutConfig.value?.contact?.title))
  const contactText = computed(() => resolveLocalizedText(aboutConfig.value?.contact?.text))

  const serviceItems = computed(() => {
    const raw = aboutConfig.value?.services?.items
    if (!Array.isArray(raw)) {
      return []
    }

    return raw
      .map((item) => resolveLocalizedText(item))
      .filter((item) => item !== '')
  })

  const hasIntroduction = computed(() => introductionText.value !== '')
  const hasServices = computed(() => servicesTitle.value !== '' || serviceItems.value.length > 0)
  const hasContactLinks = computed(() => !!(contactConfig.value?.telegram || twitterUrl.value || whatsappUrl.value || contactConfig.value?.wechat || contactConfig.value?.qq || contactConfig.value?.email))
  const hasContact = computed(() => contactTitle.value !== '' || contactText.value !== '' || hasContactLinks.value)

  onMounted(async () => {
    if (!appStore.config) {
      await appStore.loadConfig()
    }
  })

  return {
    contactConfig,
    twitterUrl,
    twitterLabel,
    whatsappUrl,
    heroTitle,
    heroSubtitle,
    introductionText,
    servicesTitle,
    contactTitle,
    contactText,
    serviceItems,
    hasIntroduction,
    hasServices,
    hasContactLinks,
    hasContact,
  }
}
