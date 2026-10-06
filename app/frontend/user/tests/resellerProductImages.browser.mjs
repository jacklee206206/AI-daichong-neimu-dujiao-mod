// Modified source snapshot by Evan | Yunqi with Codex, 2026-10-05. See repository NOTICE.md.

// Mount the real Vue component against in-memory HTTP responses; no live account or API is used.
// PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tests/resellerProductImages.browser.mjs
import assert from 'node:assert/strict'
import { mkdtemp, writeFile, rm } from 'node:fs/promises'
import { fileURLToPath, pathToFileURL } from 'node:url'
import path from 'node:path'
import { createServer } from 'vite'

if (!process.env.PLAYWRIGHT_MODULE) throw new Error('Set PLAYWRIGHT_MODULE to the installed Playwright entrypoint.')
const { chromium } = await import(pathToFileURL(process.env.PLAYWRIGHT_MODULE).href)
const root = fileURLToPath(new URL('../', import.meta.url))
const fixture = await mkdtemp(path.join(root, '.reseller-images-ui-'))
const prefix = '/' + path.basename(fixture)
const platform = ['/uploads/platform-cover.png']
const initial = {
  product: { id: 7, title: { 'zh-CN': '组件验收商品' }, images: platform, platform_images: platform, images_customized: false, is_active: true },
  product_setting: { is_listed: false, pricing_mode: 'fixed_price', fixed_price_amount: '124.00', supply_price_amount: '119.00', min_price_amount: '124.00' },
  skus: [],
}
let server, browser
try {
  await writeFile(path.join(fixture, 'index.html'), `<html><body><div id="app"></div><script type="module" src="${prefix}/main.ts"></script></body></html>`)
  await writeFile(path.join(fixture, 'main.ts'), `
    import { createApp, h, ref } from 'vue'
    import Images from '../src/components/reseller/ResellerProductImages.vue'
    import i18n, { setI18nLocale } from '../src/i18n'
    import '../src/style.css'
    await setI18nLocale('zh-CN')
    createApp({ setup() { const detail = ref(${JSON.stringify(initial)}); return () => h(Images, { detail: detail.value, onSaved: value => { detail.value = value } }) } }).use(i18n).mount('#app')
  `)
  server = await createServer({ root, server: { host: '127.0.0.1', port: 0, strictPort: false } })
  await server.listen()
  browser = await chromium.launch({ headless: true, channel: process.env.PLAYWRIGHT_BROWSER_CHANNEL || 'chrome' })
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } })
  const scriptErrors = []
  page.on('pageerror', error => scriptErrors.push(error.message))
  const saves = []
  let uploadCount = 0, failNextUpload = false, failNextSave = false, failAfterUploadCount = null
  const pixel = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO5W99kAAAAASUVORK5CYII=', 'base64')
  await page.route('**/uploads/**', route => route.fulfill({ status: 200, contentType: 'image/png', body: pixel }))
  await page.route(url => url.pathname.startsWith('/api/v1/'), async route => {
    const request = route.request()
    const pathname = new URL(request.url()).pathname
    if (pathname === '/api/v1/reseller/upload') {
      if (failNextUpload || uploadCount === failAfterUploadCount) {
        failNextUpload = false
        failAfterUploadCount = null
        return route.fulfill({ status: 500, json: { status_code: 500, msg: '模拟上传失败' } })
      }
      return route.fulfill({ json: { status_code: 0, data: { url: `/uploads/reseller/ui-${++uploadCount}.png` } } })
    }
    assert.equal(pathname, '/api/v1/reseller/product-settings/7/images')
    assert.equal(request.method(), 'PUT')
    const payload = request.postDataJSON()
    assert.deepEqual(Object.keys(payload), ['images'], 'image actions must not submit pricing or listing fields')
    if (failNextSave) {
      failNextSave = false
      return route.fulfill({ status: 500, json: { status_code: 500, msg: '模拟保存失败' } })
    }
    saves.push(payload.images)
    return route.fulfill({ json: { status_code: 0, data: { ...initial, product: { ...initial.product, images: payload.images.length ? payload.images : platform, images_customized: payload.images.length > 0 } } } })
  })
  await page.goto(server.resolvedUrls.local[0] + prefix.slice(1) + '/index.html')
  await page.getByRole('button', { name: '上传图片', exact: true }).waitFor()
  const input = page.locator('input[type="file"]')
  const upload = () => input.setInputFiles({ name: 'test.png', mimeType: 'image/png', buffer: pixel })
  const waitSaved = async count => {
    await page.waitForFunction(() => document.querySelector('section')?.getAttribute('aria-busy') === 'false')
    await page.getByRole('status').filter({ hasText: '商品图片已保存' }).waitFor()
    assert.equal(saves.length, count)
  }

  // A failed first upload must preserve inherited artwork and avoid any product write.
  failNextUpload = true
  await upload()
  await page.getByRole('alert').filter({ hasText: '模拟上传失败' }).waitFor()
  assert.equal(await page.locator('img').first().getAttribute('src'), platform[0])
  assert.equal(saves.length, 0)

  // The first successful upload replaces platform images and automatically commits only images.
  await upload(); await waitSaved(1)
  assert.deepEqual(saves[0], ['/uploads/reseller/ui-1.png'])
  await upload(); await waitSaved(2)
  assert.deepEqual(saves[1], ['/uploads/reseller/ui-1.png', '/uploads/reseller/ui-2.png'])
  await page.getByRole('button', { name: '向后移动', exact: true }).first().click(); await waitSaved(3)
  assert.deepEqual(saves[2], ['/uploads/reseller/ui-2.png', '/uploads/reseller/ui-1.png'])
  await page.getByRole('button', { name: '移除图片', exact: true }).first().click(); await waitSaved(4)
  assert.deepEqual(saves[3], ['/uploads/reseller/ui-1.png'])

  // Failed persistence keeps the uploaded draft visible and lets the same draft be retried.
  failNextSave = true
  await upload()
  await page.getByRole('alert').filter({ hasText: '模拟保存失败' }).waitFor()
  assert.equal(await page.locator('img').count(), 2)
  assert.equal(saves.length, 4)
  await page.getByRole('button', { name: '重试保存', exact: true }).click(); await waitSaved(5)
  assert.deepEqual(saves[4], ['/uploads/reseller/ui-1.png', '/uploads/reseller/ui-3.png'])

  await page.getByRole('button', { name: '恢复平台图', exact: true }).click()
  await page.getByRole('status').filter({ hasText: '已恢复平台图片' }).waitFor()
  assert.deepEqual(saves[5], [])
  assert.equal(await page.locator('img').first().getAttribute('src'), platform[0])

  // A partially uploaded batch keeps its successful files as a retryable draft.
  failAfterUploadCount = uploadCount + 1
  await input.setInputFiles([
    { name: 'first.png', mimeType: 'image/png', buffer: pixel },
    { name: 'second.png', mimeType: 'image/png', buffer: pixel },
  ])
  await page.getByRole('alert').filter({ hasText: '模拟上传失败' }).waitFor()
  assert.equal(await page.locator('img').count(), 1)
  assert.equal(await page.locator('img').first().getAttribute('src'), '/uploads/reseller/ui-4.png')
  assert.equal(saves.length, 6)
  await page.getByRole('button', { name: '重试保存', exact: true }).click(); await waitSaved(7)
  assert.deepEqual(saves[6], ['/uploads/reseller/ui-4.png'])

  // Deleting the final custom image is a restore, and the feedback must explain that.
  await page.getByRole('button', { name: '移除图片', exact: true }).click()
  await page.getByRole('status').filter({ hasText: '已恢复平台图片' }).waitFor()
  assert.deepEqual(saves[7], [])
  assert.equal(await page.locator('img').first().getAttribute('src'), platform[0])
  assert.deepEqual(initial.product_setting, { is_listed: false, pricing_mode: 'fixed_price', fixed_price_amount: '124.00', supply_price_amount: '119.00', min_price_amount: '124.00' })
  assert.deepEqual(scriptErrors, [])
  console.log('PASS mounted image component: first/partial-upload failures, automatic upload/reorder/delete saves, failed-save draft retry, final-delete restore feedback, pricing/listing isolation')
} finally {
  if (browser) await browser.close()
  if (server) await server.close()
  await rm(fixture, { recursive: true, force: true })
}
