const { test, expect } = require("@playwright/test");

test("example page runs Go WASM against opfs-wl and exposes SQLite worker capabilities", async ({ page }) => {
  const suffix = `${Date.now()}-${test.info().workerIndex}`;
  const appDb = `/playwright-go-${suffix}.db`;
  const bridgeDb = `/playwright-js-${suffix}.db`;

  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));

  await page.goto(`/?vfs=opfs-wl&db=${encodeURIComponent(appDb)}&require_persistent=true`);

  await expect(page.locator("#status")).toContainText("WebAssembly loaded successfully", {
    timeout: 60000
  });
  await expect(page.locator("#output")).toContainText("Database VFS: opfs-wl", {
    timeout: 60000
  });

  const runtimeInfo = await page.evaluate(() => window.wasmsqliteDemoInfo);
  expect(runtimeInfo).toMatchObject({
    configuredFile: appDb,
    configuredVFS: "opfs-wl",
    requirePersistent: true,
    vfsType: "opfs-wl",
    persistent: true
  });

  expect(await page.evaluate(() => window.crossOriginIsolated)).toBe(true);
  expect(await page.evaluate(() => typeof SharedArrayBuffer === "function")).toBe(true);
  expect(await page.evaluate(() => typeof Atomics.waitAsync === "function")).toBe(true);

  const init = await page.evaluate(() => window.sqliteBridge.init());
  expect(init.workerProtocolVersion).toBe(1);
  expect(init.vfsList).toContain("opfs");
  expect(init.vfsList).toContain("opfs-wl");

  const bridgeResult = await page.evaluate(async (file) => {
    const db = await window.sqliteBridge.open({
      file,
      vfs: "opfs-wl",
      requirePersistent: true,
      busyTimeout: 1000
    });
    await window.sqliteBridge.exec(
      db.dbId,
      "CREATE TABLE IF NOT EXISTS probe(id INTEGER PRIMARY KEY, label TEXT)"
    );
    await window.sqliteBridge.exec(db.dbId, "INSERT INTO probe(label) VALUES (?)", ["worker-ok"]);
    const query = await window.sqliteBridge.query(
      db.dbId,
      "SELECT label FROM probe ORDER BY id DESC LIMIT 1"
    );
    await window.sqliteBridge.close(db.dbId);
    return { db, query };
  }, bridgeDb);

  expect(bridgeResult.db).toMatchObject({
    vfsType: "opfs-wl",
    resolvedVFS: "opfs-wl",
    persistent: true
  });
  expect(bridgeResult.query.rows).toEqual([["worker-ok"]]);

  await page.getByRole("button", { name: /Run Complete CRUD Demo/i }).click();
  await expect(page.locator("#output")).toContainText("Demo completed successfully", {
    timeout: 60000
  });
  await expect(page.locator("#output")).toContainText("All CRUD operations tested");
  await expect(page.locator("#output")).toContainText("Data is persisted in OPFS");

  expect(pageErrors).toEqual([]);
});
