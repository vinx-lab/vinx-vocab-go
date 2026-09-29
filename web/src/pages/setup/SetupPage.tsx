import { useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { useLocation } from "preact-iso";
import { Button, TeamOutlined, UserOutlined, useApp } from "@/ui";
import { api, errorMessage } from "@/lib/api";
import { invalidate, setQueryData } from "@/lib/query";
import { EDITION_LABEL, type AppConfig, type Edition } from "@vinx/shared";

const OPTIONS: { value: Edition; icon: ComponentChildren; title: string; desc: string }[] = [
  { value: "personal", icon: <UserOutlined />, title: "自己用（个人版）", desc: "单人使用：没有班级、没有他人安排、没有用户管理。" },
  { value: "school", icon: <TeamOutlined />, title: "老师带班（班级版）", desc: "创建班级、批量建学生账号、布置学习计划、查看学生今日进度。" },
];

/**
 * 首次运行向导（Go 版新增，spec 0001 §2「版本」）：全新安装、未通过 VINX_EDITION 指定版本、
 * 库里还没有任何账号时，所有路由都先到这里。选好版本后进入注册页，第一个注册的账号自动成为管理员。
 * 之后可以在系统设置的「版本」卡片里随时切换（不会丢数据）。
 */
export function SetupPage() {
  const { route } = useLocation();
  const { message } = useApp();
  const [value, setValue] = useState<Edition | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const confirm = async () => {
    if (!value || submitting) return;
    setSubmitting(true);
    try {
      // 接口直接返回切换后的 /config：先换路由再写缓存，App 离开向导时直接渲染注册标签的登录页
      const cfg = await api.post<AppConfig>("/setup/edition", { edition: value });
      message.success(`已选择${EDITION_LABEL[value]}`);
      route("/login?mode=signup", true);
      setQueryData(["config"], cfg);
    } catch (e) {
      message.error(errorMessage(e, "保存失败"));
      // 例如另一个标签页已经完成了向导（403）：重新拉 /config，needsSetup 变 false 后自动离开向导
      void invalidate(["config"]);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="vx-paper" style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: 16 }}>
      <div style={{ width: "100%", maxWidth: 560 }}>
        <div className="vx-rise" style={{ textAlign: "center", marginBottom: 24 }}>
          <div className="vx-word" style={{ fontSize: 40, fontWeight: 700 }}>
            Vinx<span style={{ color: "var(--accent)" }}>·</span>Vocab
          </div>
          <div className="vx-cn" style={{ color: "var(--ink-soft)", marginTop: 4, fontSize: 16 }}>
            开始之前，先选择怎么用
          </div>
        </div>

        <div role="radiogroup" aria-label="版本" style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 14 }} className="vx-rise">
          {OPTIONS.map((opt) => {
            const selected = value === opt.value;
            return (
              <div
                key={opt.value}
                role="radio"
                aria-checked={selected}
                tabIndex={0}
                data-testid={`setup-edition-${opt.value}`}
                onClick={() => setValue(opt.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    setValue(opt.value);
                  }
                }}
                className="vx-card"
                style={{
                  cursor: "pointer",
                  padding: "20px 18px",
                  boxSizing: "border-box",
                  border: selected ? "2px solid var(--primary)" : "1px solid var(--line)",
                  background: selected ? "var(--primary-soft)" : undefined,
                }}
              >
                <div style={{ fontSize: 22, color: "var(--primary)", marginBottom: 8 }}>{opt.icon}</div>
                <div style={{ fontWeight: 700, fontSize: 16, marginBottom: 6 }}>{opt.title}</div>
                <div style={{ color: "var(--ink-soft)", fontSize: 13, lineHeight: 1.6 }}>{opt.desc}</div>
              </div>
            );
          })}
        </div>

        <div className="vx-rise" style={{ color: "var(--muted)", fontSize: 12, textAlign: "center", margin: "16px 0" }}>
          确认后进入注册页；第一个注册的账号将自动成为管理员。之后可以随时在系统设置的「版本」里切换。
        </div>

        <Button type="primary" size="large" block loading={submitting} disabled={!value} onClick={confirm}>
          确认
        </Button>
      </div>
    </div>
  );
}
