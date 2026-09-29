import type { ComponentChildren, JSX } from "preact";
import { cx } from "./util";

type P = { style?: JSX.CSSProperties; className?: string; children?: ComponentChildren };

export function Layout({ style, className, children, hasSider }: P & { hasSider?: boolean }) {
  return (
    <div class={cx("ant-layout", hasSider && "ant-layout-has-sider", className)} style={style}>
      {children}
    </div>
  );
}
function Sider({ width = 200, theme = "dark", style, className, children }: P & { width?: number; theme?: "light" | "dark" }) {
  return (
    <aside class={cx("ant-layout-sider", `ant-layout-sider-${theme}`, className)} style={{ flex: `0 0 ${width}px`, maxWidth: width, minWidth: width, width, ...(style as object) }}>
      <div class="ant-layout-sider-children">{children}</div>
    </aside>
  );
}
function Header({ style, className, children }: P) {
  return (
    <header class={cx("ant-layout-header", className)} style={style}>
      {children}
    </header>
  );
}
function Content({ style, className, children }: P) {
  return (
    <main class={cx("ant-layout-content", className)} style={style}>
      {children}
    </main>
  );
}
function Footer({ style, className, children }: P) {
  return (
    <footer class={cx("ant-layout-footer", className)} style={style}>
      {children}
    </footer>
  );
}
Layout.Sider = Sider;
Layout.Header = Header;
Layout.Content = Content;
Layout.Footer = Footer;
