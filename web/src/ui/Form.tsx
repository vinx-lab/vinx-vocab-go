import { cloneElement, createContext, isValidElement, toChildArray, type ComponentChildren, type JSX, type VNode } from "preact";
import { useContext, useEffect, useLayoutEffect, useMemo, useReducer, useRef } from "preact/hooks";
import { Col, Row } from "./Grid";
import { cx, valueOf } from "./util";

type Values = Record<string, any>;

export interface RuleObject {
  required?: boolean;
  message?: string;
  type?: "email" | "string" | "number" | "url";
  min?: number;
  max?: number;
  len?: number;
  pattern?: RegExp;
  whitespace?: boolean;
  validator?: (rule: RuleObject, value: any) => Promise<void> | void;
}
export type Rule = RuleObject | ((form: FormInstance<any>) => RuleObject);

interface ItemReg {
  name: string;
  label?: ComponentChildren;
  rules: Rule[];
  dependencies: string[];
}

type Listener = (changed: string[] | null) => void;

/** 表单实例：用法与 antd FormInstance 一致（getFieldValue / setFieldsValue / resetFields / submit / validateFields …） */
export class FormInstance<T extends Values = Values> {
  private values: Values = {};
  private initial: Values = {};
  private errors: Record<string, string[]> = {};
  private items = new Map<string, ItemReg>();
  private known = new Set<string>();
  private listeners = new Set<Listener>();
  /** 每个字段的校验序号：异步校验后到的旧结果不覆盖新结果 */
  private vseq: Record<string, number> = {};
  /** 由 <Form> 注入 */
  callbacks: { onFinish?: (v: any) => void; onFinishFailed?: (e: unknown) => void; onValuesChange?: (changed: Values, all: Values) => void } = {};

  subscribe(fn: Listener) {
    this.listeners.add(fn);
    return () => void this.listeners.delete(fn);
  }
  private notify(changed: string[] | null) {
    for (const fn of [...this.listeners]) fn(changed);
  }
  /** Form 挂载时写入初始值（只填还没有值的字段） */
  _setInitial(init: Values | undefined) {
    if (!init) return;
    this.initial = { ...init };
    for (const [k, v] of Object.entries(init)) if (!(k in this.values)) this.values[k] = v;
    for (const k of Object.keys(init)) this.known.add(k);
  }
  _register(reg: ItemReg) {
    this.items.set(reg.name, reg);
    this.known.add(reg.name);
    return () => {
      if (this.items.get(reg.name) === reg) this.items.delete(reg.name);
    };
  }
  _errors(name: string) {
    return this.errors[name] ?? [];
  }
  /** 字段被用户修改 */
  _change(name: string, value: any) {
    this.values[name] = value;
    this.callbacks.onValuesChange?.({ [name]: value }, this.getFieldsValue());
    const deps = [...this.items.values()].filter((i) => i.dependencies.includes(name) && name !== i.name && this.errors[i.name] !== undefined).map((i) => i.name);
    this.notify([name]);
    void this.validateFields([name, ...deps]).catch(() => undefined);
  }

  getFieldValue = (name: string) => this.values[name];
  getFieldsValue = (_all?: boolean | string[]): T => {
    const out: Values = {};
    for (const k of this.known) out[k] = this.values[k];
    return out as T;
  };
  setFieldValue = (name: string, value: any) => this.setFieldsValue({ [name]: value } as Partial<T>);
  setFieldsValue = (patch: Partial<T>) => {
    Object.assign(this.values, patch);
    for (const k of Object.keys(patch)) this.known.add(k);
    this.notify(Object.keys(patch));
  };
  setFields = (fields: { name: string; value?: any; errors?: string[] }[]) => {
    for (const f of fields) {
      if ("value" in f) this.values[f.name] = f.value;
      if (f.errors) this.errors[f.name] = f.errors;
    }
    this.notify(fields.map((f) => f.name));
  };
  resetFields = (names?: string[]) => {
    const keys = names ?? [...new Set([...Object.keys(this.values), ...this.known])];
    for (const k of keys) {
      if (k in this.initial) this.values[k] = this.initial[k];
      else delete this.values[k];
      delete this.errors[k];
    }
    this.notify(null);
  };
  isFieldsTouched = () => false;

  async validateFields(names?: string[]): Promise<T> {
    const targets = [...this.items.values()].filter((i) => !names || names.includes(i.name));
    const failed: { name: string; errors: string[] }[] = [];
    await Promise.all(
      targets.map(async (item) => {
        const my = (this.vseq[item.name] = (this.vseq[item.name] ?? 0) + 1);
        const errs = await runRules(item, this.values[item.name], this);
        if (my === this.vseq[item.name]) this.errors[item.name] = errs;
        if (errs.length) failed.push({ name: item.name, errors: errs });
      }),
    );
    this.notify(targets.map((t) => t.name));
    if (failed.length) throw { errorFields: failed, values: this.getFieldsValue() };
    return this.getFieldsValue();
  }

  submit = () => {
    this.validateFields().then(
      (v) => this.callbacks.onFinish?.(v),
      (e) => this.callbacks.onFinishFailed?.(e),
    );
  };
}

const isEmpty = (v: any) => v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0);
const EMAIL = /^[\w.!#$%&'*+/=?^`{|}~-]+@[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?(?:\.[a-z\d](?:[a-z\d-]{0,61}[a-z\d])?)*$/i;

/** 逐条跑规则，返回全部失败信息（antd 默认 validateFirst=false，所有失败规则都显示） */
async function runRules(item: ItemReg, value: any, form: FormInstance<any>): Promise<string[]> {
  const label = typeof item.label === "string" ? item.label : item.name;
  const errs: string[] = [];
  for (const r0 of item.rules) {
    const r = typeof r0 === "function" ? r0(form) : r0;
    const fail = (d: string) => errs.push(r.message ?? d);
    if (r.required && (isEmpty(value) || (r.whitespace && typeof value === "string" && !value.trim()))) {
      fail(`请输入${label}`);
      continue;
    }
    if (r.whitespace && typeof value === "string" && value.length && !value.trim()) {
      fail(`${label}不能为空字符`);
      continue;
    }
    if (isEmpty(value) && !r.validator) continue;
    if (r.type === "email" && typeof value === "string" && !EMAIL.test(value)) {
      fail(`${label}不是一个有效的邮箱地址`);
      continue;
    }
    if (typeof value === "number") {
      if (r.min !== undefined && value < r.min) { fail(`${label}不能小于${r.min}`); continue; }
      if (r.max !== undefined && value > r.max) { fail(`${label}不能大于${r.max}`); continue; }
    } else if (typeof value === "string" || Array.isArray(value)) {
      const size = value.length;
      if (r.len !== undefined && size !== r.len) { fail(`${label}须为${r.len}个字符`); continue; }
      if (r.min !== undefined && size < r.min) { fail(`${label}最少${r.min}个字符`); continue; }
      if (r.max !== undefined && size > r.max) { fail(`${label}最多${r.max}个字符`); continue; }
    }
    if (r.pattern && typeof value === "string" && !r.pattern.test(value)) {
      fail(`${label}格式不正确`);
      continue;
    }
    if (r.validator) {
      try {
        await r.validator(r, value);
      } catch (e) {
        errs.push(r.message ?? (e as Error)?.message ?? String(e));
      }
    }
  }
  return errs;
}

export function useForm<T extends Values = Values>(form?: FormInstance<T>): [FormInstance<T>] {
  const ref = useRef<FormInstance<T>>();
  if (!ref.current) ref.current = form ?? new FormInstance<T>();
  return [ref.current];
}

/** 订阅某个字段的值（Form.useWatch） */
export function useWatch<V = any>(name: string, form?: FormInstance): V | undefined {
  const [, force] = useReducer((n: number) => n + 1, 0);
  useEffect(() => form?.subscribe((changed) => (!changed || changed.includes(name)) && force(0)), [form, name]);
  return form?.getFieldValue(name);
}

interface FormCtx {
  form: FormInstance;
  layout: "vertical" | "horizontal" | "inline";
  requiredMark: boolean;
  disabled?: boolean;
}
const Ctx = createContext<FormCtx | null>(null);

export interface FormProps<T extends Values = Values> {
  form?: FormInstance<any>;
  layout?: "vertical" | "horizontal" | "inline";
  initialValues?: Partial<T>;
  onFinish?: (values: T) => void;
  onFinishFailed?: (e: unknown) => void;
  onValuesChange?: (changed: Partial<T>, all: T) => void;
  requiredMark?: boolean;
  preserve?: boolean;
  disabled?: boolean;
  style?: JSX.CSSProperties;
  className?: string;
  name?: string;
  autoComplete?: string;
  children?: ComponentChildren;
}

export function Form<T extends Values = Values>({ form: given, layout = "horizontal", initialValues, onFinish, onFinishFailed, onValuesChange, requiredMark = true, disabled, style, className, children, autoComplete }: FormProps<T>) {
  const [form] = useForm<T>(given);
  // 初始值要在子项首次渲染前写进去
  const inited = useRef(false);
  if (!inited.current) {
    inited.current = true;
    form._setInitial(initialValues as Values);
  }
  form.callbacks = { onFinish, onFinishFailed, onValuesChange: onValuesChange as never };
  const ctx = useMemo(() => ({ form: form as FormInstance, layout, requiredMark, disabled }), [form, layout, requiredMark, disabled]);
  return (
    <Ctx.Provider value={ctx}>
      <form
        class={cx("ant-form", `ant-form-${layout}`, className)}
        style={style}
        autoComplete={autoComplete}
        onSubmit={(e) => {
          e.preventDefault();
          e.stopPropagation();
          form.submit();
        }}
        onReset={(e) => {
          e.preventDefault();
          form.resetFields();
        }}
      >
        {children}
      </form>
    </Ctx.Provider>
  );
}

export interface FormItemProps {
  name?: string;
  label?: ComponentChildren;
  rules?: Rule[];
  extra?: ComponentChildren;
  help?: ComponentChildren;
  valuePropName?: string;
  required?: boolean;
  dependencies?: string[];
  initialValue?: any;
  hidden?: boolean;
  noStyle?: boolean;
  style?: JSX.CSSProperties;
  className?: string;
  children?: ComponentChildren;
}

export function FormItem({ name, label, rules = [], extra, help, valuePropName = "value", required, dependencies = [], initialValue, hidden, noStyle, style, className, children }: FormItemProps) {
  const ctx = useContext(Ctx);
  const [, force] = useReducer((n: number) => n + 1, 0);
  const form = ctx?.form;
  const reg = useRef<ItemReg>({ name: name ?? "", label, rules, dependencies });
  reg.current.label = label;
  reg.current.rules = rules;
  reg.current.dependencies = dependencies;
  if (form && name && initialValue !== undefined && form.getFieldValue(name) === undefined) form.setFieldsValue({ [name]: initialValue });
  useLayoutEffect(() => {
    if (!form || !name) return;
    reg.current.name = name;
    const off = form._register(reg.current);
    const unsub = form.subscribe((changed) => (!changed || changed.includes(name) || dependencies.some((d) => changed.includes(d))) && force(0));
    return () => {
      off();
      unsub();
    };
  }, [form, name]);

  const errors = form && name ? form._errors(name) : [];
  const isRequired = required ?? rules.some((r) => typeof r !== "function" && r.required);
  const id = name;
  const helpNode = help ?? (errors.length ? errors : null);
  const hasError = errors.length > 0 && help === undefined;

  let control: ComponentChildren = children;
  if (form && name) {
    const only = toChildArray(children).find(isValidElement) as VNode<any> | undefined;
    if (only) {
      const value = form.getFieldValue(name);
      const orig = only.props.onChange;
      control = cloneElement(only, {
        id: only.props.id ?? id,
        [valuePropName]: valuePropName === "value" ? value : !!value,
        ...(hasError ? { status: "error", "aria-invalid": true } : {}),
        ...(isRequired ? { "aria-required": true } : {}),
        ...(ctx?.disabled ? { disabled: true } : {}),
        onChange: (arg: unknown, ...more: unknown[]) => {
          form._change(name, valueOf(arg, valuePropName === "checked" ? "checked" : "value"));
          orig?.(arg, ...more);
        },
      });
    }
  }
  if (noStyle) return <>{control}</>;
  const layout = ctx?.layout ?? "horizontal";
  const showAdditional = helpNode || extra;
  return (
    <div class={cx("ant-form-item", hasError && "ant-form-item-with-help ant-form-item-has-error", help !== undefined && "ant-form-item-with-help", `ant-form-item-${layout}`, hidden && "ant-form-item-hidden", className)} style={style}>
      <Row className="ant-form-item-row">
        {label !== undefined && label !== null && label !== false && (
          <Col className="ant-form-item-label">
            <label for={id} class={cx(isRequired && "ant-form-item-required", isRequired && ctx && !ctx.requiredMark && "ant-form-item-required-mark-hidden")} title={typeof label === "string" ? label : undefined}>
              {label}
            </label>
          </Col>
        )}
        <Col className="ant-form-item-control">
          <div class="ant-form-item-control-input">
            <div class="ant-form-item-control-input-content">{control}</div>
          </div>
          {showAdditional && (
            <div class="ant-form-item-additional" style={helpNode ? { minHeight: (Array.isArray(helpNode) ? helpNode.length : 1) * 24 + (extra ? 24 : 0) } : undefined}>
              {helpNode && (
                <div id={id ? `${id}_help` : undefined} class="ant-form-item-explain ant-form-item-explain-connected">
                  {Array.isArray(helpNode) ? helpNode.map((m) => <div class="ant-form-item-explain-error">{m}</div>) : <div>{helpNode}</div>}
                </div>
              )}
              {extra && (
                <div id={id ? `${id}_extra` : undefined} class="ant-form-item-extra">
                  {extra}
                </div>
              )}
            </div>
          )}
        </Col>
      </Row>
      {helpNode && layout !== "inline" && <div class="ant-form-item-margin-offset" style={{ marginBottom: -24 }} />}
    </div>
  );
}

Form.Item = FormItem;
Form.useForm = useForm;
Form.useWatch = useWatch;
Form.useFormInstance = () => useContext(Ctx)?.form;
