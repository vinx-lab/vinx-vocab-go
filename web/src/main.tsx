import { render } from "preact";
import dayjs from "dayjs";
import "dayjs/locale/zh-cn";
import "@/ui";
import "./styles.css";
import { App } from "./App";
import { ThemeProvider } from "./components/ThemeProvider";

dayjs.locale("zh-cn");

// 升级二进制后，已打开页面里旧的懒加载 chunk 哈希会 404（旧版基本是单包，没有这个问题）。
// Vite 的预加载失败会在 window 上派发 vite:preloadError，这里自动刷新一次让页面拿到新 chunk，
// 用户不用手动刷新。用 sessionStorage 只重试一次，避免真的网络
// 故障时无限刷新；几秒后清掉标记，给下一次真正的升级留出重试机会。
const PRELOAD_RELOAD_KEY = "vinx:preload-reload";
window.addEventListener("vite:preloadError", () => {
  if (sessionStorage.getItem(PRELOAD_RELOAD_KEY) === "1") return;
  try {
    sessionStorage.setItem(PRELOAD_RELOAD_KEY, "1");
  } catch {
    // 隐私模式等场景 sessionStorage 不可用，仍然刷新一次
  }
  window.location.reload();
});
setTimeout(() => {
  try {
    sessionStorage.removeItem(PRELOAD_RELOAD_KEY);
  } catch {
    // ignore
  }
}, 5000);

render(
  <ThemeProvider>
    <div className="ant-app">
      <App />
    </div>
  </ThemeProvider>,
  document.getElementById("root")!,
);
