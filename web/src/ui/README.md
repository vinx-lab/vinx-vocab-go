# src/ui 组件库

替代 antd 5 的自写组件。**用法（组件名、props 名、回调参数）与 antd 5 对应组件保持一致**，从旧页面迁移时一般只需要把
`from "antd"` / `from "@ant-design/icons"` 改成 `from "@/ui"`。

外观：组件输出与 antd 相同的 DOM 结构和 class（`ant-btn`、`ant-form-item` …），样式来自 `antd.css`——
它是用旧版 antd + 旧 `theme.ts` 渲染样板页后抓下来的真实样式，亮/暗两套取值不同的地方换成了 CSS 变量，
随 `<html data-theme>` 切换。所以旧 E2E 里的 `.ant-radio-button-wrapper` 这类选择器也照样能用。

- `antd.css`：生成文件，不要手改。重新生成见下文「重新生成 antd.css」。
- `ui.css`：少量补充样式（antd 用 JS 算的部分）。
- `icon/*.tsx` 与 `icons.tsx`：生成文件（`scripts/gen-icons.mjs`），每个图标一个模块，`icons.tsx` 按名字导出，如 `<FireOutlined />`；
  `icon-base.tsx` 是共用的 `Icon` / `mk`。缺图标时在 `gen-icons.mjs` 的 `NAMES` 里加名字重新生成（参数是 `@ant-design/icons-svg` 包目录）。
- 打包：`vite.config.ts` 把 `src/ui/*.tsx` 标成无副作用模块，只在懒加载页面里用到的组件和图标跟着页面分块，不进首屏包。
  因此首屏常用的和只在少数页面用的分在不同文件：`Display.tsx`（Avatar、Tag、Spin、Empty、Result）/ `Feedback.tsx`（Alert、Space、Divider、
  Typography、Timeline、Progress）；`Popup.tsx`（Tooltip、Dropdown、Menu 与定位触发）/ `Popconfirm.tsx`（Popover、Popconfirm）；
  `Overlay.tsx`（命令式 confirm、Drawer、message、useApp）/ `Modal.tsx`（Modal 组件）。组件文件里不要写导入即生效的副作用
  （`Xxx.Item = …` 这类静态属性挂载没问题，随组件一起保留）。

## 与 antd 的差异（迁移时注意）

- **Preact 没有 compat**：DOM 上 `onChange` 是原生 change 事件（失焦才触发）。组件库内部已处理，页面里直接写原生
  `<input>` 时要用 `onInput`。`className` / `class` 都能用。
- 没有 `App` / `ConfigProvider`：`App.useApp()` 换成 `useApp()`，返回 `{ message, modal }`；主题由
  `components/ThemeProvider` 设置 `data-theme`。
- `Grid.useBreakpoint()` 首次渲染就返回真实断点（antd 首帧是空对象）。
- 表单 `Form.Item` 的 `name` 只支持字符串（旧代码没有用数组路径）。
- 没有动画（弹层直接出现/消失）；Tabs 不做溢出折叠；Table 的 `scroll.y` 用 sticky 表头实现；
  DatePicker 只有日期面板（年/月按钮不切换面板，用左右箭头翻月翻年）。

## 弹层与焦点（与 antd 一致）

- 打开的 Modal / confirm / Drawer / Popconfirm / Popover / Dropdown / Select / DatePicker 登记在同一个弹层栈里（`layers.tsx`），
  **ESC 只关最上层**；Tooltip 不参与。
- Modal / confirm / Drawer 打开时焦点移进对话框，Tab / Shift+Tab 在里面循环，关闭后焦点回到打开前的元素。
- confirm（`Modal.confirm` / `modal.confirm`）打开后焦点落在「确定」按钮上（antd 的 `autoFocusButton` 默认 `"ok"`，可传 `"cancel"` / `null`），回车即确认。
  注意：页面自己在 window 上监听回车并 `preventDefault` 时（如学习页卡片阶段），回车会先被页面处理，与旧版表现相同。
- TextArea `autoSize` 的高度按 antd（rc-textarea）的方法测量：影子 textarea 复制尺寸样式，空值时按 placeholder 量，
  minRows / maxRows 用实测单行高度；`count.max` 超出时外层加 `ant-input-out-of-range`（计数变红）。
- Modal / Drawer 的层级在**打开时**分配（后打开的在上面）；Drawer 关闭后保留内容（`destroyOnHidden` 时卸载），同 antd。

## 数据层（`src/lib/query.ts`）默认值

与旧版一致（旧版的 QueryClient 由 refine 创建：`refetchOnWindowFocus: false`、`placeholderData: keepPreviousData`）：
`useApi` 默认 **不在窗口聚焦时重取**、**换 key 时保留上一个 key 的数据**（`isPlaceholderData` 为 true 期间 `isLoading` 为 false），
失败重试 3 次，`staleTime` 0。`invalidate()` / `refetch()` 会作废在途请求重新发；`clearCache()`（退出、登录态失效、换账号时自动调用）后
旧请求与旧定时器都不会写回。`useConfig` 的 `staleTime` 为 Infinity：切换版本后要 `invalidate(["config"])`，路由守卫和导航会随之更新。

## 组件与 props

以下「同 antd」指 props 名称和语义与 antd 5 相同；只列实现了的 props。

### 基础
- **Button**：`type`(primary/default/dashed/text/link) `size`(small/middle/large) `htmlType` `icon` `loading` `disabled`
  `danger` `ghost` `block` `shape`(circle/round) `href` `onClick` `className` `style` `aria-label` `title`。两个汉字自动加空格（「登 录」），同 antd。
- **Input**：`value` `defaultValue` `onChange(e)`（`e.target.value`）`onPressEnter` `placeholder` `maxLength` `size` `prefix`
  `suffix` `allowClear` `showCount` `status` `disabled` `autoFocus` `autoComplete` `inputMode` `id` `inputRef`，其余原生属性透传。
  - **Input.Password**：同 Input，带显示/隐藏切换。
  - **Input.Search**：同 Input + `onSearch(value, e, {source})`、`enterButton`、`loading`；清空时触发 `onSearch("")`。
  - **Input.TextArea**：`value` `onChange(e)` `rows` `autoSize`(true 或 `{minRows,maxRows}`) `showCount` `maxLength` `count`(`{show,max,strategy}`，antd v5 写法，`strategy` 决定计数方式如去空白) `status` `textareaRef`。
- **InputNumber**：`value` `onChange(number|null)` `min` `max` `step` `precision` `disabled` `size` `placeholder` `status`。输入中合法且在范围内即回调，失焦时按范围夹紧。
- **Select**：`value` `onChange(value, option)` `options`(`{value,label,disabled}`) `mode`("multiple") `placeholder` `showSearch`
  `optionFilterProp`("label"/"value") `filterOption`(false 或函数) `onSearch` `onSelect` `allowClear` `loading` `disabled` `size`
  `status` `notFoundContent` `maxTagCount` `open` + `onDropdownVisibleChange`/`onOpenChange`（受控展开）`popupMatchSelectWidth` `id`。
- **Checkbox**：`checked` `indeterminate` `disabled` `onChange(e)`（`e.target.checked`）`children`。
  **Checkbox.Group**：`options` `value` `onChange(values)`。
- **Radio / Radio.Button**：`value` `checked` `disabled` `children`。
  **Radio.Group**：`value` `onChange(e)`（`e.target.value`）`options` `optionType`("button") `buttonStyle`("solid") `disabled` `size` `aria-label`。
- **Segmented**：`options`（字符串或 `{label,value,disabled,icon}`）`value` `onChange(value)` `block` `size` `disabled`。
- **Switch**：`checked` `onChange(checked)` `size`("small") `disabled` `loading` `checkedChildren` `unCheckedChildren`。
- **DatePicker**：`value`(dayjs|null) `onChange(dayjs|null, string)` `placeholder` `disabledDate(d)` `allowClear` `format` `disabled` `size` `status`。
- **Upload**：`accept` `beforeUpload(file, list)` `multiple` `disabled` `children`（只选文件，不上传；旧版就是这么用的）。

### 表单
- **Form**：`form` `layout`(vertical/horizontal/inline) `initialValues` `onFinish(values)` `onFinishFailed` `onValuesChange(changed, all)`
  `requiredMark`(false 隐藏星号) `disabled` `style` `className`。
- **Form.Item**：`name` `label` `rules` `extra` `help` `valuePropName`("checked") `required` `dependencies` `initialValue` `hidden` `noStyle` `style`。
  子元素只能有一个，会被注入 `id`/`value`/`onChange`/`status`（`label` 的 `for` 指向 `id = name`，所以 `getByLabel` 可用）。
  - rules：`required` `message` `type`("email") `min` `max` `len` `pattern` `whitespace` `validator(rule, value)`，也可以是 `(form) => rule`。
- **Form.useForm()** → `[form]`；`form` 方法：`getFieldValue` `getFieldsValue` `setFieldValue` `setFieldsValue` `setFields` `resetFields` `validateFields` `submit`。
- **Form.useWatch(name, form)**。

### 布局与展示
- **Row**：`gutter`(数字或 `[h, v]`) `align` `justify` `wrap`。**Col**：`span` `xs` `sm` `md` `lg` `xl` `flex`。
- **Grid.useBreakpoint()** / **useBreakpoint()** → `{ xs, sm, md, lg, xl, xxl }`。
- **Layout**（`hasSider`）、**Layout.Sider**（`width` `theme`）、**Layout.Header**、**Layout.Content**（`<main>`）、**Layout.Footer**。
- **Card**：`title` `extra` `size`("small") `bordered` `loading` `hoverable` `styles`({header, body}) `onClick`。
- **Space**：`direction` `size`(small/middle/large/数字/[h,v]) `wrap` `align`。**Divider**：`type` `dashed` `orientation` `plain` `children`。
- **Typography.Text**：`type`(secondary/success/warning/danger) `strong` `code`；**Typography.Paragraph**、**Typography.Title**(`level`)。
- **Tag**：`color`（预设色名或任意颜色）`bordered` `closable` `onClose` `icon`。
- **Avatar**：`size`(数字/small/default/large) `icon` `shape`。
- **Alert**：`type` `showIcon` `message` `description` `action` `closable` `onClose` `banner`。
- **Spin**：`size` `tip` `spinning` `indicator`；包住子元素时显示遮罩。
- **Empty**：`image`（`Empty.PRESENTED_IMAGE_SIMPLE` / 默认插画 / 自定义）`description` `children`(底部操作)。
- **Result**：`status`(success/error/info/warning) `title` `subTitle` `extra`。
- **Progress**（条形）：`percent` `size`("small") `showInfo` `format(p)` `status` `strokeColor` `trailColor`。
- **Steps**：`current` `status` `items`(`{title,description}`) `size` `direction` `responsive`。
- **Timeline**：`items`(`{key, children, color, dot}`)。
- **List**：`dataSource` `renderItem` `loading` `bordered` `size` `split` `header` `footer` `locale.emptyText` `rowKey`；
  **List.Item**（`actions` `extra`）、**List.Item.Meta**（`avatar` `title` `description`）。
- **Collapse**：`items`(`{key,label,children,extra}`) `defaultActiveKey` `activeKey` `onChange` `ghost` `size` `bordered` `accordion`。
- **Tabs**：`items`(`{key,label,children,disabled}`) `activeKey` `defaultActiveKey` `onChange` `destroyOnHidden` `tabBarExtraContent` `size`。
- **Table**：`columns`（`title` `dataIndex` `key` `render(v, r, i)` `width` `align` `sorter`(函数) `defaultSortOrder` `sortOrder` `fixed`("left")
  `ellipsis` `className` `responsive` `onCell`）`dataSource` `rowKey` `size` `pagination`(false 或 `{pageSize,current,hideOnSinglePage,onChange,size}`)
  `scroll`(`{x, y}`) `loading` `locale.emptyText` `rowSelection`(`{selectedRowKeys, onChange, getCheckboxProps}`) `onRow` `rowClassName` `showHeader`。
- **Pagination**：`current` `pageSize` `total` `onChange(page, size)` `size`("small") `hideOnSinglePage` `align`。

### 弹层
- **Tooltip**：`title` `placement` `open` `onOpenChange`（悬停/聚焦触发）。
- **Popover**：`title` `content` `placement` `trigger`(hover/click/focus) `open` `onOpenChange`。
- **Popconfirm**：`title` `description` `onConfirm`（返回 Promise 时确定按钮转圈）`onCancel` `okText` `cancelText` `okButtonProps` `okType` `disabled` `placement` `icon`。
- **Dropdown**：`menu`(`{items, onClick({key}), selectedKeys}`) `trigger`(["click"] / ["hover"]) `placement` `open` `onOpenChange`。
- **Menu**（侧栏 inline）：`items`（`{key,label,icon,danger,disabled}`、`{type:"group",label,children}`、`{type:"divider"}`）`selectedKeys` `onClick({key})`。
- **Modal**：`open` `title` `onOk` `onCancel` `okText` `cancelText` `confirmLoading` `okButtonProps` `cancelButtonProps` `okType`
  `footer`(null 不要底部) `width` `centered` `closable` `maskClosable` `keyboard` `destroyOnHidden` `forceRender` `afterClose` `styles.body`。
  **Modal.confirm / modal.confirm**：`{title, content, okText, cancelText, okButtonProps, okType, onOk(可返回 Promise), onCancel, icon, width}`。
- **Drawer**：`open` `onClose` `placement` `width` `height` `title` `extra` `footer` `closable` `maskClosable` `styles`({body, header})。
- **message**：`message.success|error|info|warning|loading(content, duration?)`、`message.open({type, content, duration, key})`、`message.destroy()`。
- **useApp()** → `{ message, modal }`（对应 antd 的 `App.useApp()`）。

## 重新生成 antd.css

只在需要新组件样式或改主题 token 时做（需要旧仓库装好依赖，`OLD_ADMIN` 指向旧仓库的 `apps/admin`）：

0. 改了主题 token 时先 `OLD_ADMIN=<旧仓库>/apps/admin node scripts/antd-css/tokens.cjs`（生成 `tokens.json`）
1. `OLD_ADMIN=<旧仓库>/apps/admin npx vite --config scripts/antd-css/vite.config.mjs`（样板页起在 127.0.0.1:5176，`sink.tsx` 里列着渲染的组件）
2. `OUT=<临时目录> node scripts/antd-css/dump.mjs`（抓亮/暗两套规则）
3. `IN=<临时目录> node scripts/antd-css/extract.mjs`（合并成 `src/ui/antd.css`；不用的组件/变体规则在 `DROP_LIST` 里过滤）
