// 从 @ant-design/icons-svg 抽取用到的图标路径，每个图标生成一个模块 src/ui/icon/<名字>.tsx，
// 再生成按名字导出的 src/ui/icons.tsx（只取用到的，保持打包体积小）。
// 每个图标单独成模块，懒加载页面才用到的图标会跟着页面分块，不进首屏包。
// 用法：node scripts/gen-icons.mjs <icons-svg 包目录>
// 新增图标：在 NAMES 里加名字后重新运行。
import { createRequire } from "node:module";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const pkgDir = process.argv[2];
if (!pkgDir) {
  console.error("用法：node scripts/gen-icons.mjs <@ant-design/icons-svg 包目录>");
  process.exit(1);
}
const require = createRequire(import.meta.url);

export const NAMES = `ApiOutlined ArrowLeftOutlined ArrowRightOutlined BgColorsOutlined BookOutlined BulbOutlined CalendarOutlined
CheckCircleFilled CheckOutlined CloseCircleFilled CloseOutlined CopyOutlined DeleteOutlined EditOutlined FileTextOutlined FireFilled
FireOutlined HolderOutlined ImportOutlined InboxOutlined KeyOutlined LineChartOutlined LoadingOutlined LogoutOutlined MenuOutlined
MoreOutlined PauseCircleOutlined PlayCircleFilled PlayCircleOutlined PlusOutlined PrinterOutlined ProfileOutlined ReadOutlined
ReloadOutlined RightOutlined RollbackOutlined SaveOutlined ScheduleOutlined SearchOutlined SettingOutlined SolutionOutlined
SoundOutlined TeamOutlined ThunderboltOutlined UploadOutlined UserAddOutlined UserOutlined UserSwitchOutlined UsergroupAddOutlined
DownOutlined UpOutlined LeftOutlined DoubleLeftOutlined DoubleRightOutlined EyeOutlined EyeInvisibleOutlined InfoCircleFilled
ExclamationCircleFilled WarningFilled CaretUpOutlined CaretDownOutlined EllipsisOutlined SwapRightOutlined MinusOutlined`
  .split(/\s+/)
  .filter(Boolean);

const kebab = (s) => s.replace(/([a-z0-9])([A-Z])/g, "$1-$2").toLowerCase();
const HEAD = `// 由 scripts/gen-icons.mjs 生成，不要手改。数据来自 @ant-design/icons-svg（MIT）。\n`;
const dir = new URL("../src/ui/icon/", import.meta.url);
rmSync(dir, { recursive: true, force: true });
mkdirSync(dir, { recursive: true });
let barrel = HEAD + `export { Icon, mk, type IconDef, type IconProps } from "./icon-base";\n`;
for (const n of NAMES) {
  const def = require(join(pkgDir, "lib/asn", `${n}.js`)).default;
  const paths = def.icon.children.filter((c) => c.tag === "path").map((c) => c.attrs.d);
  if (paths.length !== def.icon.children.length) throw new Error(`${n} 含非 path 元素`);
  const full = def.icon.attrs.viewBox === "0 0 1024 1024" ? 1 : 0;
  if (!full && def.icon.attrs.viewBox !== "64 64 896 896") throw new Error(`${n} viewBox ${def.icon.attrs.viewBox}`);
  const mod = HEAD + `import { mk } from "../icon-base";\nexport const ${n} = /*#__PURE__*/ mk(${JSON.stringify([kebab(def.name), full, ...paths])});\n`;
  writeFileSync(new URL(`${n}.tsx`, dir), mod);
  barrel += `export { ${n} } from "./icon/${n}";\n`;
}
writeFileSync(new URL("../src/ui/icons.tsx", import.meta.url), barrel);
console.log(`生成 ${NAMES.length} 个图标`);
