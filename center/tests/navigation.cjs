const assert = require('node:assert/strict');
async function openSection(page, name) {
  const mobileToggle = page.locator('.workspace-mobile-nav-toggle');
  if (await mobileToggle.isVisible() && (await page.locator('.workspace-nav-disclosure').getAttribute('open')) === null) await mobileToggle.click();
  const button = page.getByRole('navigation', {name:'工作台导航'}).getByRole('button', {name, exact:true});
  if (await button.getAttribute('aria-expanded') !== 'true') await button.click();
}
async function openPage(page, section, name) {
  await openSection(page, section);
  await page.getByRole('navigation', {name:'工作台导航'}).getByRole('link', {name, exact:true}).click();
  await page.getByRole('heading', {name, exact:true}).waitFor();
}
async function checkCurrent(page, name) {
  assert.equal(await page.getByRole('navigation', {name:'工作台导航'}).locator('[aria-current="page"]').innerText(), name);
  assert.ok((await page.locator('.ant-breadcrumb').innerText()).includes(name));
}
module.exports = {openSection, openPage, checkCurrent};
