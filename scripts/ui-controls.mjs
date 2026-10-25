export async function chooseOption(page, label, value) {
  const control = page.getByRole("combobox", { name: label, exact: true });
  if ((await control.evaluate((element) => element.tagName)) === "SELECT")
    return control.selectOption(value);
  if ((await control.getAttribute("aria-expanded")) !== "true")
    await control.click();
  const list = page.getByRole("listbox", { name: label, exact: true });
  const option = list.locator(
    `[role="option"][data-value=${JSON.stringify(value)}]`,
  );
  await option.scrollIntoViewIfNeeded();
  await option.click();
}
