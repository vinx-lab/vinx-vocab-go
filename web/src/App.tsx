import type { ComponentType } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import { ErrorBoundary, LocationProvider, Route, Router, useLocation } from "preact-iso";
import type { AppConfig, Capability } from "@vinx/shared";
import { AppLayout } from "@/components/AppLayout";
import { Loading } from "@/components/ui";
import { useAuthCheck, useIdentity } from "@/lib/auth";
import { can, homePath } from "@/lib/perms";
import { useConfig } from "@/lib/useConfig";
import { LoginPage } from "@/pages/auth/LoginPage";
import { SetupPage } from "@/pages/setup/SetupPage";
import { ROUTES, isBarePath, type RouteDef } from "@/routes";

/** 跳转（替换当前历史记录），对应 <Navigate replace /> */
export function Redirect({ to }: { to: string }) {
  const { route } = useLocation();
  useEffect(() => route(to, true), [to]);
  return null;
}

function HomeRedirect() {
  const { data: identity } = useIdentity();
  return <Redirect to={homePath(identity)} />;
}

/** 路由守卫：能力不足或该版本没有此功能时回落到首页 */
function Guard({ def, props }: { def: RouteDef; props: Record<string, unknown> }) {
  const { data: identity } = useIdentity();
  const config = useConfig();
  if (config.isLoading) return <Loading />;
  if (def.feature && !config.features[def.feature as keyof AppConfig["features"]]) return <Redirect to={homePath(identity)} />;
  if (def.cap && !can(identity, def.cap as Capability)) return <Redirect to={homePath(identity)} />;
  const Page = def.component as ComponentType<any>;
  return <Page {...props} />;
}

const guarded = (def: RouteDef) => (props: Record<string, unknown>) => <Guard def={def} props={props} />;
const BARE = ROUTES.filter((r) => r.bare).map((r) => ({ path: r.path, component: guarded(r) }));
const FRAMED = ROUTES.filter((r) => !r.bare).map((r) => ({ path: r.path, component: guarded(r) }));

/** 懒加载页面下载期间显示 <Loading/>（对应旧版 Suspense fallback） */
function Routes({ children }: { children: ComponentChildren }) {
  const [loading, setLoading] = useState(false);
  return (
    <>
      {loading && <Loading />}
      <div style={loading ? { display: "none" } : { display: "contents" }}>
        <Router onLoadStart={() => setLoading(true)} onLoadEnd={() => setLoading(false)}>
          {children as never}
        </Router>
      </div>
    </>
  );
}

/** 登录后的区域：先校验登录态（/auth/me），未登录回 /login */
function Protected() {
  const status = useAuthCheck();
  const { path } = useLocation();
  if (status === "unknown" || status === "checking") return null;
  if (status === "out") return <Redirect to="/login" />;
  if (isBarePath(path))
    return (
      <Routes>
        {BARE.map((r) => (
          <Route path={r.path} component={r.component} />
        ))}
      </Routes>
    );
  return (
    <AppLayout>
      <Routes>
        {[
          <Route path="/" component={HomeRedirect} />,
          ...FRAMED.map((r) => <Route path={r.path} component={r.component} />),
          <Route default component={HomeRedirect} />,
        ]}
      </Routes>
    </AppLayout>
  );
}

/** 开发模式（serve --dev）的切换账号入口：按需下载，不进正常构建的首屏包 */
function DevSwitcherLoader() {
  const [Comp, setComp] = useState<ComponentType | null>(null);
  useEffect(() => {
    let alive = true;
    import("@/components/DevSwitcher").then((m) => alive && setComp(() => m.default));
    return () => {
      alive = false;
    };
  }, []);
  return Comp ? <Comp /> : null;
}

function Root() {
  const { path } = useLocation();
  const config = useConfig();
  // 首次运行：未指定 VINX_EDITION、未选过版本、库里还没有账号时，所有路由都先到向导。
  // 取 /config 期间留白（与旧版一致：旧版根部没有整页加载态，登录页 / 退出后只会短暂空白，不闪加载圈）
  if (config.isLoading) return null;
  if (config.needsSetup) return <SetupPage />;
  return (
    <>
      {path === "/login" ? <LoginPage /> : <Protected />}
      {config.dev && <DevSwitcherLoader />}
    </>
  );
}

export function App() {
  return (
    <LocationProvider>
      <ErrorBoundary>
        <Root />
      </ErrorBoundary>
    </LocationProvider>
  );
}
