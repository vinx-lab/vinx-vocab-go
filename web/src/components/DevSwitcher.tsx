/**
 * 开发模式免密切换账号（spec 0002）：右下角悬浮按钮「DEV」+ 面板。
 * 只在 /config 返回 dev=true 时由 App 懒加载，不进正常构建的首屏包。
 */
import { useEffect, useMemo, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { ROLE_LABEL } from "@vinx/shared";
import { Button, Input, Segmented, Tag } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { impersonate, restoreBrowserSession, useIdentity } from "@/lib/auth";
import { isTabSession } from "@/lib/tabSession";

interface DevUser {
  id: string;
  email: string;
  name: string;
  role: string;
  classNames: string[];
}

type Scope = "tab" | "browser";

const ROLE_ORDER = ["admin", "teacher", "student"];

const CSS = `
.vx-dev-fab{position:fixed;right:8px;bottom:calc(88px + env(safe-area-inset-bottom));z-index:2000;display:flex;align-items:center;gap:6px;max-width:46vw;height:30px;padding:0 10px;border:none;border-radius:15px;font:600 12px/30px var(--sans,system-ui);color:#fff;background:rgba(80,80,80,.72);box-shadow:0 2px 8px rgba(0,0,0,.25);cursor:pointer;opacity:.85}
.vx-dev-fab:hover{opacity:1}
.vx-dev-fab-tab{background:#d4380d;opacity:.95}
.vx-dev-fab span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.vx-dev-panel{position:fixed;right:8px;bottom:calc(124px + env(safe-area-inset-bottom));z-index:2001;width:min(360px,calc(100vw - 16px));max-height:min(560px,calc(100vh - 160px));display:flex;flex-direction:column;gap:8px;padding:12px;border:1px solid var(--line,#ddd);border-radius:8px;background:var(--surface,#fff);color:var(--ink,#222);box-shadow:0 6px 24px rgba(0,0,0,.2);font-size:13px}
.vx-dev-list{overflow:auto;flex:1;min-height:80px;margin:0 -4px}
.vx-dev-group{padding:6px 4px 2px;font-weight:600;color:var(--muted,#888);font-size:12px}
.vx-dev-row{display:block;width:100%;text-align:left;padding:6px 8px;border:none;border-radius:6px;background:none;color:inherit;cursor:pointer}
.vx-dev-row:hover{background:var(--paper-deep,#f3f3f3)}
.vx-dev-row-cur{background:var(--primary-soft,#e6f4ff)}
.vx-dev-row small{display:block;color:var(--muted,#888)}
@media print{.vx-dev-fab,.vx-dev-panel{display:none!important}}
`;

export default function DevSwitcher() {
  const { path, route } = useLocation();
  const { data: me } = useIdentity();
  const [open, setOpen] = useState(false);
  const [users, setUsers] = useState<DevUser[] | null>(null);
  const [q, setQ] = useState("");
  const [scope, setScope] = useState<Scope>("tab");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const tab = isTabSession();

  useEffect(() => {
    if (!open) return;
    api
      .get<DevUser[]>("/dev/users")
      .then((list) => {
        setUsers(list);
        setErr("");
      })
      .catch((e) => setErr(errorMessage(e, "账号列表加载失败")));
  }, [open]);

  const groups = useMemo(() => {
    const kw = q.trim().toLowerCase();
    const hit = (users ?? []).filter((u) => !kw || u.email.toLowerCase().includes(kw) || u.name.toLowerCase().includes(kw));
    const roles = [...ROLE_ORDER, ...new Set(hit.map((u) => u.role).filter((r) => !ROLE_ORDER.includes(r)))];
    return roles.map((role) => ({ role, items: hit.filter((u) => u.role === role) })).filter((g) => g.items.length);
  }, [users, q]);

  // 打印页不显示，以免印到纸上
  if (/\/print$/.test(path)) return null;

  const run = async (fn: () => Promise<string>) => {
    setBusy(true);
    setErr("");
    try {
      const to = await fn();
      setOpen(false);
      route(to);
    } catch (e) {
      setErr(errorMessage(e, "切换失败"));
    } finally {
      setBusy(false);
    }
  };

  const roleLabel = (r: string) => (ROLE_LABEL as Record<string, string>)[r] ?? r;

  return (
    <>
      <style>{CSS}</style>
      <button type="button" class={`vx-dev-fab${tab ? " vx-dev-fab-tab" : ""}`} data-testid="dev-switcher" title="开发模式：切换账号" onClick={() => setOpen((v) => !v)}>
        DEV{tab && me ? <span>· {me.name}</span> : null}
      </button>
      {open && (
        <div class="vx-dev-panel" role="dialog" aria-label="开发模式：切换账号">
          <div>
            当前身份：
            {me ? (
              <>
                <b>{me.name}</b> <Tag>{roleLabel(me.role)}</Tag>
                <Tag color={tab ? "volcano" : "blue"}>{tab ? "本标签页" : "整个浏览器"}</Tag>
              </>
            ) : (
              <span style={{ color: "var(--muted,#888)" }}>未登录</span>
            )}
          </div>
          <Segmented<Scope>
            block
            size="small"
            aria-label="切换范围"
            value={scope}
            onChange={setScope}
            options={[
              { label: "仅本标签页", value: "tab" },
              { label: "整个浏览器", value: "browser" },
            ]}
          />
          <Input size="small" allowClear placeholder="按邮箱或名字搜索" value={q} onChange={(e) => setQ(e.target.value)} />
          <div class="vx-dev-list">
            {users === null && !err && <div class="vx-dev-group">加载中…</div>}
            {users !== null && groups.length === 0 && <div class="vx-dev-group">没有匹配的账号</div>}
            {groups.map((g) => (
              <div key={g.role}>
                <div class="vx-dev-group">{roleLabel(g.role)}</div>
                {g.items.map((u) => (
                  <button
                    type="button"
                    key={u.id}
                    class={`vx-dev-row${me?.id === u.id ? " vx-dev-row-cur" : ""}`}
                    disabled={busy}
                    onClick={() => run(() => impersonate(u.id, scope))}
                  >
                    {u.name}
                    <small>
                      {u.email}
                      {u.classNames.length ? ` · ${u.classNames.join("、")}` : ""}
                    </small>
                  </button>
                ))}
              </div>
            ))}
          </div>
          {err && <div style={{ color: "var(--bad,#cf1322)" }}>{err}</div>}
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
            {tab && (
              <Button size="small" disabled={busy} onClick={() => run(restoreBrowserSession)}>
                恢复为浏览器账号
              </Button>
            )}
            <Button size="small" type="text" onClick={() => setOpen(false)}>
              关闭
            </Button>
          </div>
        </div>
      )}
    </>
  );
}
