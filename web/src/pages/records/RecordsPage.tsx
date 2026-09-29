import { PageHeader } from "@/components/ui";
import { RecordsView } from "./RecordsView";

export function RecordsPage() {
  return (
    <div className="vx-page">
      <PageHeader eyebrow="Records" title="学习记录" />
      <RecordsView />
    </div>
  );
}
