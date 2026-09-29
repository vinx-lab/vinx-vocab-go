import { createContext, type ComponentChildren } from "preact";
import { useCallback, useContext, useEffect, useMemo, useState } from "preact/hooks";
import type { ThemePref } from "@vinx/shared";
import { api } from "@/lib/api";
import { patchCachedUser } from "@/lib/auth";
import { THEME_EVENT, applyThemeToDocument, readCachedTheme, resolveTheme, setSavingTheme, systemPrefersDark, writeCachedTheme, type ResolvedTheme } from "@/lib/theme";

interface ThemeContextValue {
  /** 用户选择：跟随系统 / 浅色 / 深色 */
  pref: ThemePref;
  /** 实际生效的主题 */
  resolved: ResolvedTheme;
  /** 立即切换并保存到账号；保存失败会恢复原值并抛出错误 */
  setPref: (pref: ThemePref) => Promise<void>;
  saving: boolean;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

/** 外观：读缓存 → 设 <html data-theme>（组件库与 styles.css 的色板都按它切换）；账号值由 auth 同步进缓存后经事件通知这里 */
export function ThemeProvider({ children }: { children: ComponentChildren }) {
  const [pref, setPrefState] = useState<ThemePref>(readCachedTheme);
  const [systemDark, setSystemDark] = useState(systemPrefersDark);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const onChange = (e: Event) => setPrefState((e as CustomEvent<ThemePref>).detail);
    // 其他标签页改了缓存也跟着变
    const onStorage = () => setPrefState(readCachedTheme());
    window.addEventListener(THEME_EVENT, onChange);
    window.addEventListener("storage", onStorage);
    const mq = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-color-scheme: dark)") : null;
    const onSystem = (e: MediaQueryListEvent) => setSystemDark(e.matches);
    mq?.addEventListener?.("change", onSystem);
    return () => {
      window.removeEventListener(THEME_EVENT, onChange);
      window.removeEventListener("storage", onStorage);
      mq?.removeEventListener?.("change", onSystem);
    };
  }, []);

  const resolved = resolveTheme(pref, systemDark);
  useEffect(() => applyThemeToDocument(resolved), [resolved]);

  const setPref = useCallback(
    async (next: ThemePref) => {
      const prev = pref;
      if (next === prev) return;
      writeCachedTheme(next);
      setSavingTheme(next);
      setSaving(true);
      try {
        await api.put("/auth/profile", { theme: next });
        patchCachedUser({ theme: next });
      } catch (e) {
        writeCachedTheme(prev);
        throw e;
      } finally {
        setSavingTheme(null);
        setSaving(false);
      }
    },
    [pref],
  );

  const value = useMemo(() => ({ pref, resolved, setPref, saving }), [pref, resolved, setPref, saving]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useThemePref(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error("useThemePref 必须在 ThemeProvider 内使用");
  return ctx;
}
