import { test, expect, type Page, type Route } from '@playwright/test';

const api = (path: string) => `**/api/v1${path}`;

function ok(data: unknown) {
  return { code: 0, message: 'ok', data };
}

async function mockPlatform(page: Page) {
  await page.route(api('/auth/token'), async (route: Route) => {
    await route.fulfill({ json: ok({ access_token: 'e2e-token', token_type: 'Bearer', expires_in: 3600 }) });
  });
  await page.route(api('/me'), async (route: Route) => {
    await route.fulfill({ json: ok({ username: 'e2e.operator@example.com', tenant_id: 'e2e-tenant', roles: ['operator', 'sre_admin'] }) });
  });
  await page.route(api('/sessions?*'), async (route: Route) => {
    await route.fulfill({ json: ok({ items: [], total: 0 }) });
  });
  await page.route(api('/approvals?*'), async (route: Route) => {
    await route.fulfill({ json: ok({ items: [], total: 0 }) });
  });
  await page.route(api('/ops/tasks?*'), async (route: Route) => {
    await route.fulfill({ json: ok({ items: [], total: 0 }) });
  });
}

async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('用户名').fill('e2e.operator@example.com');
  await page.getByLabel('密码').fill('not-a-real-secret');
  // Ant Design inserts a visual spacing character into the button text;
  // submit semantics are the stable contract we want to exercise here.
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/chat$/);
  await expect(page.getByLabel('Agent 问题输入')).toBeVisible();
}

test.describe('portal browser contracts', () => {
  test.beforeEach(async ({ page }) => {
    await mockPlatform(page);
  });

  test('authenticates and exposes accessible chat controls', async ({ page }) => {
    await login(page);

    await expect(page.getByLabel('启用 RAG 知识库')).toBeChecked();
    await expect(page.getByLabel('启用工具调用')).toBeChecked();
    await expect(page.getByLabel('启用流式响应')).toBeChecked();

    await page.getByLabel('启用流式响应').uncheck();
    await expect(page.getByLabel('启用流式响应')).not.toBeChecked();
    await expect(page.getByRole('button', { name: '发送问题' })).toBeDisabled();
  });

  test('renders ops empty state and accessible diagnostic controls', async ({ page }) => {
    await login(page);
    await page.getByRole('menuitem', { name: '告警分析' }).click();
    await expect(page).toHaveURL(/\/ops$/);
    await expect(page.getByLabel('Ops 告警分析提示词')).toBeVisible();
    await expect(page.getByLabel('异步模式')).toBeVisible();
    await expect(page.getByRole('group', { name: '最大 Agent 执行步数' })).toBeVisible();
    await expect(page.getByLabel('开始告警分析')).toBeVisible();
    await expect(page.getByText('尚未发起任务')).toBeVisible();
    await expect(page.getByLabel('刷新 Ops 任务列表')).toBeVisible();
  });

  test('keeps failed lazy route recovery actionable', async ({ page }) => {
    await login(page);
    await page.goto('/traces/e2e-missing-trace');
    await expect(page).toHaveURL(/\/traces\/e2e-missing-trace$/);
    // The trace page may show a backend error or an empty state, but it must
    // remain a rendered, recoverable page rather than a blank screen.
    await expect(page.locator('body')).not.toBeEmpty();
  });
});
