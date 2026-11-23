// Only edits browser form state; never saves configuration or starts a probe.
const assert = require("node:assert/strict");

module.exports = async function checkRTCEditorClose(page) {
  const create = page.getByRole("button", { name: "创建探测点任务", exact: true });
  const editor = page.getByRole("dialog", { name: "创建探测点任务配置", exact: true });
  const name = editor.getByPlaceholder("例如：生产环境 Web 探测");
  const confirmation = page.locator(".rtc-confirm");
  async function expectConfirmation() {
    await confirmation.waitFor();
    await page.waitForFunction(() => {
      const dialog = document.querySelector(".rtc-confirm");
      return dialog && getComputedStyle(dialog).opacity === "1" &&
        dialog.getBoundingClientRect().width > 300;
    });
  }
  async function close(method) {
    if (method === "close") await editor.locator(".ant-modal-close").click();
    else if (method === "cancel") await editor.getByRole("button", { name: "取消", exact: true }).click();
    else {
      await name.focus();
      await page.keyboard.press("Escape");
    }
  }
  for (const method of ["close", "cancel", "escape"]) {
    await create.click();
    await editor.waitFor();
    await close(method);
    await editor.waitFor({ state: "hidden" });
    assert.equal(await confirmation.count(), 0, "untouched form closes directly");

    await create.click();
    await name.fill("关闭回归验证 · " + method);
    await close(method);
    await expectConfirmation();
    await confirmation.getByRole("button", { name: "继续编辑", exact: true }).click();
    await confirmation.waitFor({ state: "hidden" });
    assert.equal(await name.inputValue(), "关闭回归验证 · " + method);
    await close(method);
    await expectConfirmation();
    await confirmation.getByRole("button", { name: "放弃修改", exact: true }).click();
    await editor.waitFor({ state: "hidden" });
    await confirmation.waitFor({ state: "hidden" });
  }
};
