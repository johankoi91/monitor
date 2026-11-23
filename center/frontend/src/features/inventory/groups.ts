export async function resolveGroup(
  value: string,
  groups: { code: string; name: string }[],
) {
  const name = value.trim().normalize("NFC");
  if (!name || new TextEncoder().encode(name).length > 128)
    throw new Error("请填写分组名，名称过长时请缩短后重试");
  const existing = groups.find((g) => g.name === name || g.code === name);
  if (existing) return existing;
  const code = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$/.test(name)
    ? name
    : "group-" +
      Array.from(
        new Uint8Array(
          await crypto.subtle.digest("SHA-256", new TextEncoder().encode(name)),
        ),
        (byte) => byte.toString(16).padStart(2, "0"),
      )
        .join("")
        .slice(0, 24);
  if (groups.some((g) => g.code === code && g.name !== name))
    throw new Error("分组名与现有分组冲突，请调整名称");
  return { code, name };
}
