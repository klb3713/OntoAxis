import { chromium } from '@playwright/test';
const browser = await chromium.launch({ headless: true, executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const steps = [];
const logs = [];
page.on('console', (m) => { if (m.type() === 'error') logs.push(m.text()); });
async function step(name, fn) { try { await fn(); steps.push({name, ok:true}); } catch(e) { steps.push({name, ok:false, error:String(e).slice(0,500)}); throw e; } }
async function shot(name) { await page.screenshot({ path: `/tmp/ontoaxis-e2e-${name}.png`, fullPage: true }); }
await step('login', async () => {
  await page.goto('http://localhost:18082/', { waitUntil: 'networkidle' });
  await page.getByRole('button').filter({ hasText: 'Keycloak' }).click();
  await page.locator('#username').fill('admin');
  await page.locator('#password').fill('admin123');
  await page.locator('#kc-login').click();
  await page.waitForURL(/\/projects/, { timeout: 15000 });
});
await step('projects-list', async () => {
  await page.waitForTimeout(1200);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail Demo')) throw new Error('Trail Demo missing');
  await shot('projects');
});
await step('ontology-list', async () => {
  await page.goto('http://localhost:18082/ontology', { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail')) throw new Error('Trail type missing');
  if (!body.includes('4')) throw new Error('property count missing');
  await shot('ontology');
});
await step('object-explorer', async () => {
  await page.goto('http://localhost:18082/object-explorer', { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail')) throw new Error('object explorer type missing');
  await shot('object-explorer');
});
await step('datasets-page', async () => {
  await page.goto('http://localhost:18082/datasets', { waitUntil: 'networkidle' });
  await page.waitForTimeout(800);
  await shot('datasets');
});
await step('query-execute', async () => {
  await page.goto('http://localhost:18082/queries', { waitUntil: 'networkidle' });
  await page.locator('textarea.sql-editor').fill('SELECT 42 AS answer');
  await page.getByRole('button').filter({ hasText: '执行查询' }).click();
  await page.waitForTimeout(1500);
  const body = await page.locator('body').innerText();
  if (!body.includes('42')) throw new Error('query result 42 missing');
  await shot('queries');
});
console.log(JSON.stringify({ steps, logs }, null, 2));
await browser.close();
