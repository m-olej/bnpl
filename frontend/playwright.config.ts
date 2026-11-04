import { defineConfig, devices } from '@playwright/test';

// Point the Go services at the local (or CI) MySQL. Onboarding/authorize/refund
// all write, so the database must be migrated before the run.
const DB_DSN =
  process.env.E2E_DB_DSN ??
  process.env.DB_DSN ??
  'bnpl:bnplpw@tcp(127.0.0.1:3306)/bnpl?parseTime=true&loc=UTC&charset=utf8mb4&collation=utf8mb4_0900_ai_ci';

// Playwright replaces the whole environment when `env` is supplied, so merge
// with process.env to keep PATH/HOME so `go` and `npm` resolve.
const backendEnv = { ...process.env, DB_DSN, LOG_LEVEL: 'error' };

export default defineConfig({
  testDir: './e2e',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL: 'http://127.0.0.1:5173',
    trace: 'on-first-retry',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: [
    {
      command: 'go run ./cmd/underwriting',
      cwd: '../backend',
      port: 8082,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: backendEnv,
    },
    {
      command: 'go run ./cmd/checkout',
      cwd: '../backend',
      port: 8081,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: backendEnv,
    },
    {
      command: 'npm run dev -- --host 127.0.0.1 --port 5173',
      port: 5173,
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
  ],
});
