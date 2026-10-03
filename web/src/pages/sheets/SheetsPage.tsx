import { Link } from "@/lib/router";
import { Button, PlusOutlined } from "@/ui";
import { PageHeader } from "@/components/ui";
import { SheetsList } from "./SheetsList";

/** 单词单：打印不熟的词带去学校看，回家在电脑上测试 */
export function SheetsPage() {
  return (
    <div className="vx-page">
      <PageHeader
        eyebrow="纸上复习"
        title="单词单"
        extra={
          <Link to="/sheets/new">
            <Button type="primary" icon={<PlusOutlined />}>
              生成单词单
            </Button>
          </Link>
        }
      >
        挑出最近容易错、容易忘的词，打印成对折自测表；在学校看完，回家在「今日」里测一遍。也可以出「默写单」：看中文写英文，手写后由家长或老师批改录入。
      </PageHeader>
      <SheetsList />
    </div>
  );
}
