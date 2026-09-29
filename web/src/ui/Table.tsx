import type { ComponentChildren, JSX } from "preact";
import { useEffect, useState } from "preact/hooks";
import { Checkbox } from "./Checkbox";
import { Empty, Spin } from "./Display";
import { useBreakpoint, type Screens } from "./Grid";
import { CaretDownOutlined, CaretUpOutlined, DoubleLeftOutlined, DoubleRightOutlined, LeftOutlined, RightOutlined } from "./icons";
import { cx } from "./util";

export interface PaginationProps {
  current?: number;
  pageSize?: number;
  total?: number;
  onChange?: (page: number, pageSize: number) => void;
  size?: "small" | "default";
  showSizeChanger?: boolean;
  hideOnSinglePage?: boolean;
  align?: "start" | "center" | "end";
  className?: string;
  style?: JSX.CSSProperties;
}

/** 页码列表：与 rc-pagination 相同的省略规则（当前页前后各 2 页，首尾页常显） */
export function pageList(current: number, all: number): (number | "prev5" | "next5")[] {
  if (all <= 7) return Array.from({ length: all }, (_, i) => i + 1);
  let left = Math.max(1, current - 2);
  let right = Math.min(all, current + 2);
  if (current - 1 <= 2) right = 1 + 4;
  if (all - current <= 2) left = all - 4;
  const out: (number | "prev5" | "next5")[] = [];
  if (left > 1) out.push(1);
  if (left > 2) out.push("prev5");
  for (let i = left; i <= right; i++) out.push(i);
  if (right < all - 1) out.push("next5");
  if (right < all) out.push(all);
  return out;
}

export function Pagination({ current = 1, pageSize = 10, total = 0, onChange, size, hideOnSinglePage, align, className, style }: PaginationProps) {
  const all = Math.max(1, Math.ceil(total / pageSize));
  if (hideOnSinglePage && all <= 1) return null;
  const go = (p: number) => {
    const n = Math.min(all, Math.max(1, p));
    if (n !== current) onChange?.(n, pageSize);
  };
  const list = pageList(current, all);
  return (
    <ul class={cx("ant-pagination", align && `ant-pagination-${align}`, size === "small" && "ant-pagination-mini", className)} style={style}>
      <li title="上一页" tabIndex={current <= 1 ? undefined : 0} class={cx("ant-pagination-prev", current <= 1 && "ant-pagination-disabled")} aria-disabled={current <= 1} onClick={() => go(current - 1)}>
        <button class="ant-pagination-item-link" type="button" tabIndex={-1} disabled={current <= 1}>
          <LeftOutlined />
        </button>
      </li>
      {list.map((p) =>
        typeof p === "number" ? (
          <li title={String(p)} class={cx("ant-pagination-item", `ant-pagination-item-${p}`, p === current && "ant-pagination-item-active")} tabIndex={0} onClick={() => go(p)}>
            <a rel="nofollow">{p}</a>
          </li>
        ) : (
          <li title={p === "prev5" ? "向前 5 页" : "向后 5 页"} tabIndex={0} class={cx(p === "prev5" ? "ant-pagination-jump-prev" : "ant-pagination-jump-next", `ant-pagination-${p === "prev5" ? "jump-prev" : "jump-next"}-custom-icon`)} onClick={() => go(current + (p === "prev5" ? -5 : 5))}>
            <a class="ant-pagination-item-link">
              <div class="ant-pagination-item-container">
                {p === "prev5" ? <DoubleLeftOutlined className="ant-pagination-item-link-icon" /> : <DoubleRightOutlined className="ant-pagination-item-link-icon" />}
                <span class="ant-pagination-item-ellipsis">•••</span>
              </div>
            </a>
          </li>
        ),
      )}
      <li title="下一页" tabIndex={current >= all ? undefined : 0} class={cx("ant-pagination-next", current >= all && "ant-pagination-disabled")} aria-disabled={current >= all} onClick={() => go(current + 1)}>
        <button class="ant-pagination-item-link" type="button" tabIndex={-1} disabled={current >= all}>
          <RightOutlined />
        </button>
      </li>
    </ul>
  );
}

export type SortOrder = "ascend" | "descend" | null;
export interface ColumnType<T> {
  title?: ComponentChildren;
  dataIndex?: keyof T | string;
  key?: string;
  render?: (value: any, record: T, index: number) => ComponentChildren;
  width?: number | string;
  align?: "left" | "center" | "right";
  sorter?: boolean | ((a: T, b: T) => number);
  defaultSortOrder?: SortOrder;
  sortOrder?: SortOrder;
  fixed?: "left" | "right" | boolean;
  ellipsis?: boolean;
  className?: string;
  /** 小于这些断点时隐藏（antd 语义：只在列出的断点显示） */
  responsive?: ("xs" | "sm" | "md" | "lg" | "xl" | "xxl")[];
  onCell?: (record: T, index: number) => JSX.HTMLAttributes<HTMLTableCellElement>;
}

export interface TableProps<T> {
  columns: ColumnType<T>[];
  dataSource?: T[];
  rowKey?: string | ((r: T) => string);
  size?: "small" | "middle" | "large";
  pagination?: false | Omit<PaginationProps, "total"> & { total?: number };
  scroll?: { x?: number | string | true; y?: number | string };
  loading?: boolean;
  locale?: { emptyText?: ComponentChildren };
  rowSelection?: { selectedRowKeys: (string | number)[]; onChange?: (keys: (string | number)[], rows: T[]) => void; getCheckboxProps?: (r: T) => { disabled?: boolean } };
  onRow?: (record: T, index: number) => JSX.HTMLAttributes<HTMLTableRowElement>;
  rowClassName?: string | ((r: T, i: number) => string);
  showHeader?: boolean;
  className?: string;
  style?: JSX.CSSProperties;
  bordered?: boolean;
}

const get = (r: any, di: any) => (di === undefined ? undefined : Array.isArray(di) ? di.reduce((o, k) => o?.[k], r) : r?.[di]);

function visibleBy(screens: Screens, resp?: ColumnType<unknown>["responsive"]) {
  if (!resp) return true;
  return resp.some((bp) => screens[bp]);
}

export function Table<T>({ columns, dataSource = [], rowKey = "key", size = "large", pagination, scroll, loading, locale, rowSelection, onRow, rowClassName, showHeader = true, className, style }: TableProps<T>) {
  const screens = useBreakpoint();
  const cols = columns.filter((c) => visibleBy(screens, c.responsive));
  const keyOf = (r: T, i: number): string | number => (typeof rowKey === "function" ? rowKey(r) : ((r as Record<string, unknown>)[rowKey] as string) ?? i);
  const initialSort = cols.findIndex((c) => c.defaultSortOrder);
  const [sort, setSort] = useState<{ i: number; order: SortOrder }>({ i: initialSort, order: initialSort >= 0 ? cols[initialSort].defaultSortOrder ?? null : null });
  const controlledSort = cols.findIndex((c) => c.sortOrder !== undefined);
  const s = controlledSort >= 0 ? { i: controlledSort, order: cols[controlledSort].sortOrder ?? null } : sort;
  let rows = dataSource;
  const sc = s.i >= 0 ? cols[s.i] : undefined;
  if (sc && s.order && typeof sc.sorter === "function") {
    const f = sc.sorter;
    rows = [...rows].sort((a, b) => (s.order === "ascend" ? f(a, b) : -f(a, b)));
  }
  const pg = pagination === false ? null : { pageSize: 10, ...(pagination ?? {}) };
  const [innerPage, setPage] = useState(pg?.current ?? 1);
  const page = pg?.current ?? innerPage;
  const total = pg?.total ?? rows.length;
  useEffect(() => {
    const max = Math.max(1, Math.ceil(total / (pg?.pageSize ?? 10)));
    if (innerPage > max) setPage(max);
  }, [total]);
  const shown = pg && pg.total === undefined ? rows.slice((page - 1) * pg.pageSize!, page * pg.pageSize!) : rows;
  const toggleSort = (i: number) => {
    const next: SortOrder = s.i !== i ? "ascend" : s.order === "ascend" ? "descend" : s.order === "descend" ? null : "ascend";
    setSort({ i, order: next });
  };
  const sel = rowSelection;
  const selectable = shown.filter((r) => !sel?.getCheckboxProps?.(r)?.disabled);
  const allOn = !!sel && selectable.length > 0 && selectable.every((r, i) => sel.selectedRowKeys.includes(keyOf(r, i)));
  const someOn = !!sel && selectable.some((r, i) => sel.selectedRowKeys.includes(keyOf(r, i)));
  const toggleAll = () => {
    if (!sel) return;
    const keys: (string | number)[] = selectable.map((r, i) => keyOf(r, i));
    const next = allOn ? sel.selectedRowKeys.filter((k) => !keys.includes(k)) : [...new Set([...sel.selectedRowKeys, ...keys])];
    sel.onChange?.(next, dataSource.filter((r, i) => next.includes(keyOf(r, i))));
  };
  const toggleOne = (k: string | number) => {
    if (!sel) return;
    const next = sel.selectedRowKeys.includes(k) ? sel.selectedRowKeys.filter((x) => x !== k) : [...sel.selectedRowKeys, k];
    sel.onChange?.(next, dataSource.filter((r, i) => next.includes(keyOf(r, i))));
  };
  const fixedLeft = cols.some((c) => c.fixed === "left" || c.fixed === true);
  // 左固定列用 sticky，累计 left
  let acc = sel ? 32 : 0;
  const leftOf = cols.map((c) => {
    if (c.fixed === "left" || c.fixed === true) {
      const l = acc;
      acc += typeof c.width === "number" ? c.width : 0;
      return l;
    }
    return undefined;
  });
  const lastFixed = leftOf.reduce((last, v, i) => (v !== undefined ? i : last), -1);
  const scrollX = scroll?.x;
  const tableStyle: JSX.CSSProperties = scrollX !== undefined ? { width: scrollX === true ? "auto" : scrollX, minWidth: "100%", tableLayout: scrollX === true ? "auto" : "fixed" } : { tableLayout: "auto" };
  const empty = shown.length === 0;
  const fixCell = (i: number) => (leftOf[i] !== undefined ? { position: "sticky" as const, left: leftOf[i] } : undefined);
  const fixCls = (i: number) => cx(leftOf[i] !== undefined && "ant-table-cell-fix-left", i === lastFixed && "ant-table-cell-fix-left-last");
  const table = (
    <table style={tableStyle}>
      <colgroup>
        {sel && <col class="ant-table-selection-col" style={{ width: 32 }} />}
        {cols.map((c) => (
          <col style={c.width !== undefined ? { width: c.width } : undefined} />
        ))}
      </colgroup>
      {showHeader && (
        <thead class="ant-table-thead" style={scroll?.y ? { position: "sticky", top: 0, zIndex: 3 } : undefined}>
          <tr>
            {sel && (
              <th class={cx("ant-table-cell", "ant-table-selection-column", fixedLeft && "ant-table-cell-fix-left")} scope="col" style={fixedLeft ? { position: "sticky", left: 0 } : undefined}>
                <div class="ant-table-selection">
                  <Checkbox aria-label="Select all" checked={allOn} indeterminate={!allOn && someOn} onChange={toggleAll} />
                </div>
              </th>
            )}
            {cols.map((c, i) => {
              const sortable = !!c.sorter;
              const on = s.i === i && s.order;
              return (
                <th
                  aria-sort={on ? (s.order === "ascend" ? "ascending" : "descending") : undefined}
                  aria-label={sortable && typeof c.title === "string" ? c.title : undefined}
                  class={cx("ant-table-cell", fixCls(i), on && "ant-table-column-sort", sortable && "ant-table-column-has-sorters", c.align && `ant-table-cell-align-${c.align}`, c.ellipsis && "ant-table-cell-ellipsis", c.className)}
                  tabIndex={sortable ? 0 : undefined}
                  scope="col"
                  style={{ ...fixCell(i), ...(c.align ? { textAlign: c.align } : {}) }}
                  onClick={sortable ? () => toggleSort(i) : undefined}
                >
                  {sortable ? (
                    <div class="ant-table-column-sorters">
                      <span class="ant-table-column-title">{c.title}</span>
                      <span class="ant-table-column-sorter ant-table-column-sorter-full">
                        <span class="ant-table-column-sorter-inner" aria-hidden="true">
                          <CaretUpOutlined className={cx("ant-table-column-sorter-up", on && s.order === "ascend" && "active")} />
                          <CaretDownOutlined className={cx("ant-table-column-sorter-down", on && s.order === "descend" && "active")} />
                        </span>
                      </span>
                    </div>
                  ) : (
                    c.title
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
      )}
      <tbody class="ant-table-tbody">
        {empty ? (
          <tr class="ant-table-placeholder">
            <td colSpan={cols.length + (sel ? 1 : 0)} class="ant-table-cell">
              {locale?.emptyText !== undefined ? locale.emptyText : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} className="ant-empty-normal" />}
            </td>
          </tr>
        ) : (
          shown.map((r, ri) => {
            const k = keyOf(r, ri);
            const extra = onRow?.(r, ri) ?? {};
            const checked = !!sel?.selectedRowKeys.includes(k);
            return (
              <tr {...extra} class={cx("ant-table-row", "ant-table-row-level-0", checked && "ant-table-row-selected", typeof rowClassName === "function" ? rowClassName(r, ri) : rowClassName, extra.class as string)} data-row-key={k}>
                {sel && (
                  <td class={cx("ant-table-cell", "ant-table-selection-column", fixedLeft && "ant-table-cell-fix-left")} style={fixedLeft ? { position: "sticky", left: 0 } : undefined}>
                    <Checkbox checked={checked} disabled={sel.getCheckboxProps?.(r)?.disabled} onChange={() => toggleOne(k)} />
                  </td>
                )}
                {cols.map((c, i) => {
                  const v = get(r, c.dataIndex);
                  const cell = c.onCell?.(r, ri) ?? {};
                  const content = c.render ? c.render(v, r, ri) : (v as ComponentChildren);
                  return (
                    <td {...cell} class={cx("ant-table-cell", fixCls(i), s.i === i && s.order && "ant-table-column-sort", c.ellipsis && "ant-table-cell-ellipsis", c.className)} style={{ ...fixCell(i), ...(c.align ? { textAlign: c.align } : {}), ...((cell.style as object) ?? {}) }}>
                      {content}
                    </td>
                  );
                })}
              </tr>
            );
          })
        )}
      </tbody>
    </table>
  );
  return (
    <div class={cx("ant-table-wrapper", className)} style={style}>
      <Spin spinning={!!loading}>
        <div class={cx("ant-table", size === "small" && "ant-table-small", size === "middle" && "ant-table-middle", empty && "ant-table-empty", fixedLeft && "ant-table-has-fix-left", scroll?.y && "ant-table-fixed-header", scrollX !== undefined && "ant-table-scroll-horizontal")}>
          <div class="ant-table-container">
            <div class={scroll?.y ? "ant-table-body" : "ant-table-content"} style={scroll?.y ? { overflow: "auto", maxHeight: scroll.y } : scrollX !== undefined ? { overflow: "auto hidden" } : undefined}>
              {table}
            </div>
          </div>
        </div>
        {pg && !(pg.hideOnSinglePage && total <= pg.pageSize!) && total > 0 && (
          <Pagination
            className="ant-table-pagination"
            align="end"
            size={pg.size ?? (size === "large" ? "default" : "small")}
            current={page}
            pageSize={pg.pageSize}
            total={total}
            onChange={(p, ps) => {
              setPage(p);
              pg.onChange?.(p, ps);
            }}
          />
        )}
      </Spin>
    </div>
  );
}
