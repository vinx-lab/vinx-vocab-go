import { PageHeader } from "@/components/ui";
import { WordsView } from "./WordsView";

export function WordsPage() {
  return (
    <div className="vx-page">
      <PageHeader eyebrow="My Words" title="我的单词" />
      <WordsView />
    </div>
  );
}
