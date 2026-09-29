/** A4 对折表每页行数（页眉 + 30 行 + 页脚正好一页） */
export const ROWS_PER_PAGE = 30;

export function paginate<T>(rows: T[], perPage = ROWS_PER_PAGE): { rows: T[]; start: number }[] {
  const pages: { rows: T[]; start: number }[] = [];
  for (let start = 0; start < rows.length; start += perPage) pages.push({ rows: rows.slice(start, start + perPage), start });
  return pages;
}
