import { chromium } from '@playwright/test';
const browser = await chromium.launch({ headless: true, executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' });
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage();
const responses = [];
page.on('response', async (r) => {
  if (r.url().includes('/api/')) {
    let body = '';
    try { body = (await r.text()).slice(0, 300); } catch {}
    responses.push({ url: r.url(), status: r.status(), body });
  }
});
page.on('console', (m) => { if (m.type() === 'error') console.log('CONSOLE', m.text()); });
await page.goto('http://localhost:18082/', { waitUntil: 'networkidle' });
await page.getByRole('button').filter({ hasText: 'Keycloak' }).click();
await page.locator('#username').fill('admin');
await page.locator('#password').fill('admin123');
await page.locator('#kc-login').click();
await page.waitForURL(/\/projects/, { timeout: 15000 });
await page.waitForTimeout(3000);
console.log('PROJECTS BODY', (await page.locator('body').innerText()).slice(0, 800));
await page.goto('http://localhost:18082/ontology', { waitUntil: 'networkidle' });
await page.waitForTimeout(2000);
console.log('ONTOLOGY BODY', (await page.locator('body').innerText()).slice(0, 800));
console.log('RESPONSES', JSON.stringify(responses, null, 2));
await browser.close();
