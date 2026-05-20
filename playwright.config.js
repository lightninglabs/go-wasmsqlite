const { defineConfig } = require("@playwright/test");

module.exports = defineConfig({
  testDir: "./tests",
  timeout: 120000,
  expect: {
    timeout: 30000
  },
  use: {
    baseURL: "http://localhost:8081",
    trace: "on-first-retry"
  },
  webServer: {
    command: "npm run serve:example",
    url: "http://localhost:8081",
    reuseExistingServer: !process.env.CI,
    timeout: 120000
  },
  projects: [
    {
      name: "chromium",
      use: {
        browserName: "chromium"
      }
    }
  ]
});
