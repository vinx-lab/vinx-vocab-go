import { PageHeader } from "@/components/ui";

/** 尚未迁移的页面占位（B2–B4 逐个替换成正式页面） */
export function Placeholder({ title }: { title: string }) {
  return (
    <div className="vx-page">
      <PageHeader eyebrow="待迁移" title={title}>
        这个页面还没有从旧版迁移过来。
      </PageHeader>
    </div>
  );
}
