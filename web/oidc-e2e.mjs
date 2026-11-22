import { chromium } from '@playwright/test';

const browser = await chromium.launch({
  headless: true,
  executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
});
const context = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await context.newPage();
const logs = [];
page.on('console', (msg) => logs.push(`console:${msg.type()}:${msg.text()}`));
page.on('pageerror', (err) => logs.push(`pageerror:${err.message}`));

const steps = [];
async function step(name, fn) {
  try { await fn(); steps.push({ name, ok: true }); }
  catch (error) { steps.push({ name, ok: false, error: String(error) }); throw error; }
}

await step('open-web', async () => {
  await page.goto('http://localhost:18082/', { waitUntil: 'networkidle' });
  await page.screenshot({ path: '/tmp/ontoaxis-web-entry.png', fullPage: true });
});
await step('click-oidc-login', async () => {
  await page.getByRole('button').filter({ hasText: 'Keycloak' }).first().click();
  await page.waitForURL(/localhost:18083\/realms\/ontoaxis\/protocol\/openid-connect\/auth/, { timeout: 15000 });
});
await step('keycloak-login', async () => {
  await page.locator('#username').fill('admin');
  await page.locator('#password').fill('admin123');
  await page.locator('#kc-login').click();
  await page.waitForURL(/localhost:18082\/auth\/callback/, { timeout: 15000 });
});
await step('projects-page', async () => {
  await page.waitForURL(/localhost:18082\/projects/, { timeout: 15000 });
  await page.waitForLoadState('networkidle');
  await page.screenshot({ path: '/tmp/ontoaxis-web-projects.png', fullPage: true });
  const text = await page.locator('body').innerText();
  if (!/Projects|项目/.test(text)) throw new Error('projects page did not render');
});
await step('navigate-core-pages', async () => {
  for (const [path, name] of [['/ontology','ontology'], ['/object-explorer','object-explorer'], ['/datasets','datasets'], ['/queries','queries']]) {
    await page.goto(`http://localhost:18082${path}`, { waitUntil: 'networkidle' });
    await page.screenshot({ path: `/tmp/ontoaxis-web-${name}.png`, fullPage: true });
    const text = await page.locator('body').innerText();
    if (/Cannot read|is not a function|Failed to fetch|401|403/.test(text)) throw new Error(`${path} error text: ${text.slice(0,200)}`);
  }
});
console.log(JSON.stringify({ steps, logs: logs.slice(-40) }, null, 2));
await browser.close();
