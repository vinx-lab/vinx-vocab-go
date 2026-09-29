import { useEffect, useState } from "preact/hooks";
import { useLocation } from "preact-iso";
import { Button, Form, Input, Segmented, useApp } from "@/ui";
import { api } from "@/lib/api";
import { check, login, setSignedIn } from "@/lib/auth";
import { homePath } from "@/lib/perms";
import type { User } from "@vinx/shared";

interface LoginValues {
  email: string;
  password: string;
}

interface SignupValues {
  name: string;
  email: string;
  password: string;
  inviteCode?: string;
}

/** 登录 / 注册：账号可以是邮箱，也可以是老师批量创建的学号 */
export function LoginPage() {
  const { route, query } = useLocation();
  const { message } = useApp();
  const [mode, setMode] = useState<"login" | "signup">(query.mode === "signup" ? "signup" : "login");
  // 首次运行向导跳到 /login?mode=signup 时，登录页可能已经挂着（守卫先把人送到了 /login），这里跟上
  useEffect(() => {
    if (query.mode === "signup") setMode("signup");
  }, [query.mode]);
  const [loggingIn, setLoggingIn] = useState(false);
  const [signingUp, setSigningUp] = useState(false);

  const onLogin = async (values: LoginValues) => {
    setLoggingIn(true);
    try {
      route(await login(values.email, values.password));
    } catch (e) {
      message.error((e as Error)?.message ?? "登录失败");
    } finally {
      setLoggingIn(false);
    }
  };

  const onSignup = async (values: SignupValues) => {
    setSigningUp(true);
    try {
      const data = await api.post<{ user: User }>("/auth/signup", { ...values, inviteCode: values.inviteCode?.trim() || undefined });
      setSignedIn(data.user);
      await check();
      message.success(values.inviteCode ? "注册成功，已加入班级" : "注册成功");
      route(homePath(data.user));
    } catch (e) {
      message.error((e as Error).message);
    } finally {
      setSigningUp(false);
    }
  };

  return (
    <div className="vx-paper" style={{ minHeight: "100vh", display: "grid", placeItems: "center", padding: 16 }}>
      <div style={{ width: "100%", maxWidth: 400 }}>
        <div className="vx-rise" style={{ textAlign: "center", marginBottom: 24 }}>
          <div className="vx-word" style={{ fontSize: 40, fontWeight: 700 }}>
            Vinx<span style={{ color: "var(--accent)" }}>·</span>Vocab
          </div>
          <div className="vx-cn" style={{ color: "var(--ink-soft)", marginTop: 4, fontSize: 16 }}>
            每天一点点，单词记得牢
          </div>
        </div>
        <div className="vx-card vx-rise" style={{ padding: 24, animationDelay: "80ms" }}>
          <Segmented
            block
            value={mode}
            onChange={(v) => setMode(v as "login" | "signup")}
            options={[
              { label: "登录", value: "login" },
              { label: "注册", value: "signup" },
            ]}
            style={{ marginBottom: 20 }}
          />
          {mode === "login" ? (
            <Form<LoginValues> layout="vertical" onFinish={onLogin} requiredMark={false}>
              <Form.Item name="email" label="账号" rules={[{ required: true, message: "请输入邮箱或学号" }]}>
                <Input size="large" placeholder="邮箱或老师分配的学号" autoComplete="username" />
              </Form.Item>
              <Form.Item name="password" label="密码" rules={[{ required: true, message: "请输入密码" }]}>
                <Input.Password size="large" autoComplete="current-password" />
              </Form.Item>
              <Button type="primary" htmlType="submit" size="large" block loading={loggingIn}>
                登录
              </Button>
            </Form>
          ) : (
            <Form<SignupValues> layout="vertical" onFinish={onSignup} requiredMark={false}>
              <Form.Item name="name" label="姓名" rules={[{ required: true, message: "请输入姓名" }]}>
                <Input size="large" maxLength={50} />
              </Form.Item>
              <Form.Item name="email" label="邮箱" rules={[{ required: true, type: "email", message: "请输入有效邮箱" }]}>
                <Input size="large" autoComplete="email" />
              </Form.Item>
              <Form.Item name="password" label="密码" rules={[{ required: true, min: 6, message: "密码至少 6 位" }]}>
                <Input.Password size="large" autoComplete="new-password" />
              </Form.Item>
              <Form.Item name="inviteCode" label="班级邀请码（选填）" extra="向老师要 6 位邀请码，注册后自动加入班级">
                <Input size="large" maxLength={10} style={{ textTransform: "uppercase", fontFamily: "var(--mono)" }} />
              </Form.Item>
              <Button type="primary" htmlType="submit" size="large" block loading={signingUp}>
                注册并开始学习
              </Button>
            </Form>
          )}
        </div>
        {import.meta.env.DEV && (
          <div style={{ color: "var(--muted)", fontSize: 12, textAlign: "center", marginTop: 16, lineHeight: 1.7 }}>
            演示账号（密码 dev123456）
            <br />
            student@vinx.test · teacher@vinx.test · admin@vinx.test
          </div>
        )}
      </div>
    </div>
  );
}
