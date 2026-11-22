import { chromium } from '@playwright/test';

const baseUrl = process.env.E2E_BASE_URL ?? 'http://localhost:18082';
const executablePath = process.env.PLAYWRIGHT_EXECUTABLE_PATH;
const username = process.env.E2E_KEYCLOAK_USER ?? 'admin';
const password = process.env.E2E_KEYCLOAK_PASSWORD ?? 'admin123';

const browser = await chromium.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const steps = [];
const errors = [];
page.on('console', (message) => {
  if (message.type() === 'error') errors.push(message.text());
});

async function step(name, fn) {
  try {
    await fn();
    steps.push({ name, ok: true });
  } catch (error) {
    steps.push({ name, ok: false, error: String(error).slice(0, 500) });
    throw error;
  }
}

async function shot(name) {
  await page.screenshot({ path: `/tmp/ontoaxis-e2e-${name}.png`, fullPage: true });
}

await step('login', async () => {
  await page.goto(`${baseUrl}/`, { waitUntil: 'networkidle' });
  await page.getByRole('button').filter({ hasText: 'Keycloak' }).click();
  await page.locator('#username').fill(username);
  await page.locator('#password').fill(password);
  await page.locator('#kc-login').click();
  await page.waitForURL(/\/projects/, { timeout: 20_000 });
});

await step('projects-list', async () => {
  await page.waitForTimeout(1200);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail Demo')) throw new Error('Trail Demo missing');
  await shot('projects');
});

await step('ontology-list', async () => {
  await page.goto(`${baseUrl}/ontology`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail')) throw new Error('Trail type missing');
  await shot('ontology');
});

await step('object-explorer', async () => {
  await page.goto(`${baseUrl}/object-explorer`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(1000);
  const body = await page.locator('body').innerText();
  if (!body.includes('Trail')) throw new Error('object explorer type missing');
  await shot('object-explorer');
});

await step('datasets-page', async () => {
  await page.goto(`${baseUrl}/datasets`, { waitUntil: 'networkidle' });
  await page.waitForTimeout(800);
  await shot('datasets');
});

await step('query-execute', async () => {
  await page.goto(`${baseUrl}/queries`, { waitUntil: 'networkidle' });
  await page.locator('textarea.sql-editor').fill('SELECT 42 AS answer');
  await page.getByRole('button').filter({ hasText: '执行查询' }).click();
  await page.waitForTimeout(1500);
  const body = await page.locator('body').innerText();
  if (!body.includes('42')) throw new Error('query result 42 missing');
  await shot('queries');
});

console.log(JSON.stringify({ steps, errors }, null, 2));
await browser.close();
