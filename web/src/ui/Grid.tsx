import { createContext, type ComponentChildren, type JSX } from "preact";
import { useContext, useEffect, useState } from "preact/hooks";
import { cx } from "./util";

type Gutter = number | [number, number];
const GutterCtx = createContext<[number, number]>([0, 0]);

export interface RowProps {
  gutter?: Gutter;
  align?: "top" | "middle" | "bottom" | "stretch";
  justify?: "start" | "end" | "center" | "space-around" | "space-between" | "space-evenly";
  wrap?: boolean;
  style?: JSX.CSSProperties;
  className?: string;
  children?: ComponentChildren;
}

export function Row({ gutter = 0, align, justify, wrap = true, style, className, children }: RowProps) {
  const g: [number, number] = Array.isArray(gutter) ? gutter : [gutter, 0];
  const s: JSX.CSSProperties = {};
  if (g[0]) {
    s.marginLeft = -g[0] / 2;
    s.marginRight = -g[0] / 2;
  }
  if (g[1]) s.rowGap = g[1];
  return (
    <GutterCtx.Provider value={g}>
      <div class={cx("ant-row", align && `ant-row-${align}`, justify && `ant-row-${justify}`, !wrap && "ant-row-no-wrap", className)} style={{ ...s, ...(style as object) }}>
        {children}
      </div>
    </GutterCtx.Provider>
  );
}

type Span = number | { span?: number };
export interface ColProps {
  span?: number;
  xs?: Span;
  sm?: Span;
  md?: Span;
  lg?: Span;
  xl?: Span;
  flex?: string | number;
  style?: JSX.CSSProperties;
  className?: string;
  children?: ComponentChildren;
}

const flexStyle = (flex: string | number) => {
  if (typeof flex === "number") return `${flex} ${flex} auto`;
  if (/^\d+(\.\d+)?(px|em|rem|%)$/.test(flex)) return `0 0 ${flex}`;
  return flex;
};

export function Col({ span, xs, sm, md, lg, xl, flex, style, className, children }: ColProps) {
  const g = useContext(GutterCtx);
  const sizes = { xs, sm, md, lg, xl };
  const cls = Object.entries(sizes)
    .filter(([, v]) => v !== undefined)
    .map(([k, v]) => `ant-col-${k}-${typeof v === "number" ? v : v!.span}`);
  const s: JSX.CSSProperties = {};
  if (g[0]) {
    s.paddingLeft = g[0] / 2;
    s.paddingRight = g[0] / 2;
  }
  if (flex !== undefined) {
    s.flex = flexStyle(flex);
    s.minWidth = 0;
  }
  return (
    <div class={cx("ant-col", span !== undefined && `ant-col-${span}`, ...cls, className)} style={{ ...s, ...(style as object) }}>
      {children}
    </div>
  );
}

/** antd 断点：xs <576, sm ≥576, md ≥768, lg ≥992, xl ≥1200, xxl ≥1600 */
const BP = { sm: 576, md: 768, lg: 992, xl: 1200, xxl: 1600 } as const;
export type Screens = Partial<Record<"xs" | keyof typeof BP, boolean>>;

function read(): Screens {
  if (typeof window === "undefined") return {};
  const w = window.innerWidth;
  return { xs: w < BP.sm, sm: w >= BP.sm, md: w >= BP.md, lg: w >= BP.lg, xl: w >= BP.xl, xxl: w >= BP.xxl };
}

export function useBreakpoint(): Screens {
  const [s, set] = useState(read);
  useEffect(() => {
    const on = () => {
      const n = read();
      set((p) => (JSON.stringify(p) === JSON.stringify(n) ? p : n));
    };
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);
  return s;
}

export const Grid = { useBreakpoint };
