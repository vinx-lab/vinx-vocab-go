import { useState } from "preact/hooks";
import type { ComponentChildren, JSX } from "preact";
import { useLocation } from "preact-iso";
import {
  Avatar,
  BgColorsOutlined,
  BookOutlined,
  Button,
  CalendarOutlined,
  CheckOutlined,
  Drawer,
  Dropdown,
  FileTextOutlined,
  FireOutlined,
  Grid,
  Layout,
  LineChartOutlined,
  LogoutOutlined,
  Menu,
  MenuOutlined,
  PrinterOutlined,
  ProfileOutlined,
  ReadOutlined,
  SettingOutlined,
  SolutionOutlined,
  TeamOutlined,
  UserOutlined,
  useApp,
} from "@/ui";
import { ROLE_LABEL, THEME_PREFS, type AppConfig, type Capability, type ThemePref } from "@vinx/shared";
import { can } from "@/lib/perms";
import { errorMessage } from "@/lib/api";
import { logout, useIdentity } from "@/lib/auth";
import { THEME_LABEL } from "@/lib/theme";
import { useConfig } from "@/lib/useConfig";
import { useThemePref } from "./ThemeProvider";

const { Sider, Header, Content } = Layout;

interface NavItem {
  key: string;
  /** 需要的能力 */
  cap?: Capability;
  /** 需要的版本功能（个人版会隐藏） */
  feature?: keyof AppConfig["features"];
  icon: JSX.Element;
  label: string;
  mobile?: boolean;
}

/** 导航按权限过滤；学习相关在前，教学与管理在后 */
const LEARN_NAV: NavItem[] = [
  { key: "/today", cap: "study", icon: <FireOutlined />, label: "今日", mobile: true },
  { key: "/plans", cap: "plans", icon: <CalendarOutlined />, label: "学习计划", mobile: true },
  { key: "/words", cap: "study", icon: <ReadOutlined />, label: "我的单词", mobile: true },
  { key: "/passages", cap: "study", icon: <FileTextOutlined />, label: "短文巩固", mobile: true },
  { key: "/sheets", cap: "study", icon: <PrinterOutlined />, label: "单词单" },
  { key: "/records", cap: "study", icon: <LineChartOutlined />, label: "学习记录", mobile: true },
  { key: "/books", cap: "books.read", icon: <BookOutlined />, label: "词书" },
];

const TEACH_NAV: NavItem[] = [{ key: "/classes", cap: "classes", feature: "classes", icon: <TeamOutlined />, label: "班级" }];

const ADMIN_NAV: NavItem[] = [
  { key: "/admin/users", cap: "users", feature: "multiUser", icon: <SolutionOutlined />, label: "用户" },
  // 系统设置不限版本：个人版的唯一账号就是管理员（K38）
  { key: "/admin/settings", cap: "system", icon: <SettingOutlined />, label: "系统设置" },
];

export function AppLayout({ children }: { children: ComponentChildren }) {
  const { data: identity } = useIdentity();
  const location = useLocation();
  const screens = Grid.useBreakpoint();
  const [drawer, setDrawer] = useState(false);
  const isMobile = !screens.md;

  const config = useConfig();
  const visible = (items: NavItem[]) => items.filter((i) => (!i.cap || can(identity, i.cap)) && (!i.feature || config.features[i.feature]));
  const learn = visible(LEARN_NAV);
  const teach = visible(TEACH_NAV);
  const admin = visible(ADMIN_NAV);
  const all = [...learn, ...teach, ...admin];
  const selected = all.map((i) => i.key).find((k) => location.path === k || location.path.startsWith(`${k}/`));

  const toMenu = (items: NavItem[]) => items.map((i) => ({ key: i.key, icon: i.icon, label: <a href={i.key}>{i.label}</a> }));
  const groups = [
    ...(learn.length ? [{ type: "group" as const, key: "g-learn", label: "学习", children: toMenu(learn) }] : []),
    ...(teach.length ? [{ type: "group" as const, key: "g-teach", label: "教学", children: toMenu(teach) }] : []),
    ...(admin.length ? [{ type: "group" as const, key: "g-admin", label: "管理", children: toMenu(admin) }] : []),
  ];

  const brand = (
    <a href="/" style={{ display: "flex", alignItems: "baseline", gap: 8, padding: "20px 20px 12px", textDecoration: "none" }}>
      <span className="vx-word" style={{ fontSize: 22, fontWeight: 700, color: "var(--ink)" }}>
        Vinx<span style={{ color: "var(--accent)" }}>·</span>Vocab
      </span>
    </a>
  );

  const menu = <Menu mode="inline" selectedKeys={selected ? [selected] : []} items={groups} onClick={() => setDrawer(false)} />;

  const { pref: themePref, setPref: setThemePref } = useThemePref();
  const { message } = useApp();
  const userMenu = {
    items: [
      { key: "/profile", icon: <UserOutlined />, label: "个人中心" },
      { type: "divider" as const },
      {
        type: "group" as const,
        key: "g-theme",
        label: (
          <span>
            <BgColorsOutlined /> 外观
          </span>
        ),
        children: THEME_PREFS.map((p) => ({
          key: `theme:${p}`,
          label: THEME_LABEL[p],
          icon: <CheckOutlined style={{ visibility: themePref === p ? "visible" : "hidden" }} />,
        })),
      },
      { type: "divider" as const },
      { key: "logout", icon: <LogoutOutlined />, label: "退出登录", danger: true },
    ],
    selectedKeys: [`theme:${themePref}`],
    onClick: ({ key }: { key: string }) => {
      if (key === "logout") {
        // 开发模式「仅本标签页」下退出后回到浏览器账号的首页，否则到登录页
        void logout().then((to) => location.route(to));
        return;
      }
      if (key.startsWith("theme:")) {
        setThemePref(key.slice(6) as ThemePref).catch((e) => message.error(errorMessage(e, "外观保存失败")));
        return;
      }
      location.route(key);
    },
  };

  const roleName = identity ? ROLE_LABEL[identity.role] : "";
  const mobileTabs = learn.filter((i) => i.mobile);

  return (
    // --tabbar-h：手机底部导航占的高度，页面里贴底的元素（如批改页的提交栏）据此让开
    <Layout style={{ minHeight: "100vh", ["--tabbar-h" as string]: isMobile && mobileTabs.length > 1 ? "calc(64px + env(safe-area-inset-bottom))" : "0px" }} className="vx-paper" hasSider={!isMobile}>
      {!isMobile && (
        <Sider width={216} theme="light" style={{ background: "var(--paper-deep)", borderRight: "1px solid var(--line)", position: "sticky", top: 0, height: "100vh", overflow: "auto" }}>
          {brand}
          {menu}
        </Sider>
      )}
      <Drawer open={isMobile && drawer} onClose={() => setDrawer(false)} placement="left" width={240} styles={{ body: { padding: 0, background: "var(--paper-deep)" } }} closable={false}>
        {brand}
        {menu}
      </Drawer>
      <Layout>
        <Header
          style={{
            background: "var(--header-bg)",
            backdropFilter: "blur(8px)",
            borderBottom: "1px solid var(--line)",
            padding: isMobile ? "0 12px" : "0 24px",
            display: "flex",
            alignItems: "center",
            gap: 12,
            position: "sticky",
            top: 0,
            zIndex: 10,
            height: 56,
            lineHeight: "56px",
          }}
        >
          {isMobile && <Button type="text" icon={<MenuOutlined />} aria-label="打开菜单" onClick={() => setDrawer(true)} />}
          {isMobile && (
            <span className="vx-word" style={{ fontWeight: 700, fontSize: 18 }}>
              Vinx<span style={{ color: "var(--accent)" }}>·</span>Vocab
            </span>
          )}
          <div style={{ flex: 1 }} />
          <Dropdown menu={userMenu} trigger={["click"]}>
            <Button type="text" style={{ display: "flex", alignItems: "center", gap: 8, height: 40 }}>
              <Avatar size={28} style={{ background: "var(--primary)" }}>
                {identity?.name?.slice(0, 1) ?? <ProfileOutlined />}
              </Avatar>
              {!isMobile && (
                <span>
                  {identity?.name}
                  {roleName && <span style={{ color: "var(--muted)", marginLeft: 6, fontSize: 12 }}>{roleName}</span>}
                </span>
              )}
            </Button>
          </Dropdown>
        </Header>
        <Content>{children}</Content>
      </Layout>
      {isMobile && mobileTabs.length > 1 && (
        <nav
          aria-label="主导航"
          style={{
            position: "fixed",
            bottom: 0,
            left: 0,
            right: 0,
            display: "flex",
            background: "var(--tabbar-bg)",
            borderTop: "1px solid var(--line)",
            paddingBottom: "env(safe-area-inset-bottom)",
            zIndex: 20,
          }}
        >
          {mobileTabs.map((t) => {
            const active = selected === t.key;
            return (
              <a
                key={t.key}
                href={t.key}
                style={{ flex: 1, display: "flex", flexDirection: "column", alignItems: "center", padding: "8px 0 6px", fontSize: 11, color: active ? "var(--primary)" : "var(--muted)", gap: 2 }}
              >
                <span style={{ fontSize: 18 }}>{t.icon}</span>
                {t.label}
              </a>
            );
          })}
        </nav>
      )}
    </Layout>
  );
}
