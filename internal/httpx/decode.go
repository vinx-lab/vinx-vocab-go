package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// Opt 请求体字段：区分「缺省」「null」「有值」（zod 的 optional / nullable 语义靠它判断）。
//
//	Set=false        字段缺省
//	Set=true,Null    字段为 null
//	Set=true         字段有值 Val
type Opt[T any] struct {
	Set  bool
	Null bool
	Val  T
}

// Some 有值的 Opt（测试与内部构造用）。
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, Val: v} }

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

// Present 有值（非缺省且非 null）。
func (o Opt[T]) Present() bool { return o.Set && !o.Null }

// Validatable 请求体类型实现它，Decode 在字段解码后调用 Validate 做 zod 风格校验。
type Validatable interface {
	Validate(v *V)
}

// Decode 把请求体解析为 T（对应旧 parseBody(schema, body)）：
//   - 无请求体或 JSON null 视为 {}（旧版 body ?? {}）；
//   - 根不是对象 → VALIDATION details { body: "Expected object, received <类型>" }；
//   - T 为结构体时逐个字段解码，类型不符记为 "Expected <期望>, received <实际>"，其余字段继续；
//   - *T 实现 Validatable 时调用 Validate；
//
// 任一问题都返回 VALIDATION「参数校验失败」+ 字段级 details（同一路径后写覆盖前写，与旧版 toDetails 一致）。
func Decode[T any](r *http.Request) (T, error) {
	var out T
	v := &V{}
	fields, err := objectOf(bodyOf(r))
	if err != nil {
		v.Add("body", err.Error())
		return out, v.Err()
	}
	decodeInto(reflect.ValueOf(&out).Elem(), fields, "", v)
	if vv, ok := any(&out).(Validatable); ok {
		vv.Validate(v)
	}
	return out, v.Err()
}

// DecodeValue 与 Decode 相同，但直接用给定的 JSON 文本（测试或非 HTTP 场景）。
func DecodeValue[T any](raw []byte) (T, error) {
	var out T
	v := &V{}
	fields, err := objectOf(&parsedBody{kind: bodyJSON, raw: raw})
	if err != nil {
		v.Add("body", err.Error())
		return out, v.Err()
	}
	decodeInto(reflect.ValueOf(&out).Elem(), fields, "", v)
	if vv, ok := any(&out).(Validatable); ok {
		vv.Validate(v)
	}
	return out, v.Err()
}

func objectOf(pb *parsedBody) (map[string]json.RawMessage, error) {
	switch pb.kind {
	case bodyNone:
		return map[string]json.RawMessage{}, nil
	case bodyText:
		return nil, fmt.Errorf("Expected object, received string")
	}
	raw := bytes.TrimSpace(pb.raw)
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	if raw[0] != '{' {
		return nil, fmt.Errorf("Expected object, received %s", jsonKind(raw))
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("Expected object, received %s", jsonKind(raw))
	}
	return m, nil
}

// jsonKind JSON 值在 zod 里的类型名。
func jsonKind(raw []byte) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "undefined"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "boolean"
	case 'n':
		return "null"
	}
	return "number"
}

// decodeInto 结构体按 json 标签逐字段解码；非结构体整体解码。
func decodeInto(dst reflect.Value, fields map[string]json.RawMessage, prefix string, v *V) {
	if dst.Kind() != reflect.Struct {
		b, _ := json.Marshal(fields)
		if err := json.Unmarshal(b, dst.Addr().Interface()); err != nil {
			v.Add(pathJoin(prefix, "body"), err.Error())
		}
		return
	}
	t := dst.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag := f.Tag.Get("json"); tag != "" {
			if tag == "-" {
				continue
			}
			name = strings.Split(tag, ",")[0]
		}
		raw, ok := fields[name]
		if !ok {
			continue
		}
		fv := dst.Field(i)
		path := pathJoin(prefix, name)
		if decodeOptField(fv, raw, path, v) {
			continue
		}
		if err := json.Unmarshal(raw, fv.Addr().Interface()); err != nil {
			v.addType(pathJoin(prefix, typeErrorPath(name, err)), typeErrorMessage(raw, fv.Type(), err))
		}
	}
}

// decodeOptField 对 Opt[T] 字段里 T 是结构体或基本类型数组的情况做递归解码，
// 让嵌套字段（如 targets.classIds）和数组元素（如 targets.classIds.0）的类型错误
// 都能定位到与 zod 一致的路径，而不是整体归到外层字段名下。
// 命中（返回 true）时调用方不再走整体 json.Unmarshal 分支；命中但字段本身缺省/结构不对时
// 也会直接记录问题，调用方同样不需要再处理。未命中（如 T 是字符串/数字/布尔，或数组元素是结构体，
// 后者各处已有手写的逐项校验）返回 false，沿用原来的整体解码 + 单一路径报错。
func decodeOptField(fv reflect.Value, raw json.RawMessage, path string, v *V) bool {
	t := fv.Type()
	if t.Kind() != reflect.Struct || !strings.HasPrefix(t.Name(), "Opt[") {
		return false
	}
	valField := fv.FieldByName("Val")
	setField := fv.FieldByName("Set")
	nullField := fv.FieldByName("Null")
	if !valField.IsValid() || !setField.IsValid() || !nullField.IsValid() {
		return false
	}
	trimmed := bytes.TrimSpace(raw)

	switch valField.Kind() {
	case reflect.Struct:
		setField.SetBool(true)
		if string(trimmed) == "null" {
			nullField.SetBool(true)
			return true
		}
		if len(trimmed) == 0 || trimmed[0] != '{' {
			v.addType(path, fmt.Sprintf("Expected object, received %s", jsonKind(trimmed)))
			return true
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &nested); err != nil {
			v.addType(path, fmt.Sprintf("Expected object, received %s", jsonKind(trimmed)))
			return true
		}
		decodeInto(valField, nested, path, v)
		return true

	case reflect.Slice:
		elemKind := valField.Type().Elem().Kind()
		switch elemKind {
		case reflect.String, reflect.Bool, reflect.Float64, reflect.Struct:
		default:
			return false
		}
		setField.SetBool(true)
		if string(trimmed) == "null" {
			nullField.SetBool(true)
			return true
		}
		if len(trimmed) == 0 || trimmed[0] != '[' {
			v.addType(path, fmt.Sprintf("Expected array, received %s", jsonKind(trimmed)))
			return true
		}
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			v.addType(path, fmt.Sprintf("Expected array, received %s", jsonKind(trimmed)))
			return true
		}
		out := reflect.MakeSlice(valField.Type(), len(items), len(items))
		if elemKind == reflect.Struct {
			// 元素是对象（如 import 的 units / entries）：逐个元素按字段递归解码，类型错误定位到 units.0.entries 这样的路径
			for i, item := range items {
				elemPath := fmt.Sprintf("%s.%d", path, i)
				it := bytes.TrimSpace(item)
				var nested map[string]json.RawMessage
				if len(it) == 0 || it[0] != '{' || json.Unmarshal(it, &nested) != nil {
					v.addType(elemPath, fmt.Sprintf("Expected object, received %s", jsonKind(it)))
					continue
				}
				decodeInto(out.Index(i), nested, elemPath, v)
			}
			valField.Set(out)
			return true
		}
		ok := true
		for i, item := range items {
			elemPath := fmt.Sprintf("%s.%d", path, i)
			if err := json.Unmarshal(item, out.Index(i).Addr().Interface()); err != nil {
				v.addType(elemPath, typeErrorMessage(item, valField.Type().Elem(), err))
				ok = false
			}
		}
		if ok {
			valField.Set(out)
		}
		return true

	default:
		return false
	}
}

func pathJoin(prefix, p string) string {
	if prefix == "" {
		return p
	}
	return prefix + "." + p
}

func typeErrorPath(name string, err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		if strings.HasPrefix(te.Field, name) {
			return te.Field
		}
		return name + "." + te.Field
	}
	return name
}

func typeErrorMessage(raw json.RawMessage, target reflect.Type, err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		expected := zodTypeName(te.Type)
		received := te.Value
		switch received {
		case "bool":
			received = "boolean"
		case "number", "string", "array", "object":
		default:
			if strings.HasPrefix(received, "number") {
				received = "number"
			}
		}
		if expected == "integer" && received == "number" {
			return "Expected integer, received float"
		}
		return fmt.Sprintf("Expected %s, received %s", expected, received)
	}
	return fmt.Sprintf("Expected %s, received %s", zodTypeName(target), jsonKind(raw))
}

// zodTypeName Go 类型在 zod 消息里的叫法。
func zodTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct && strings.HasPrefix(t.Name(), "Opt[") {
		if f, ok := t.FieldByName("Val"); ok {
			return zodTypeName(f.Type)
		}
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "object"
	}
	return t.Kind().String()
}

// DecodeQuery 查询参数（旧 parseQuery）：每个参数取第一个值作为字符串字段解码，其余规则同 Decode。
// 查询参数字段一律声明为 Opt[string]；数字用 V.CoerceInt（z.coerce.number()）。
func DecodeQuery[T any](r *http.Request) (T, error) {
	var out T
	v := &V{}
	fields := map[string]json.RawMessage{}
	for k, vals := range r.URL.Query() {
		if len(vals) == 0 {
			continue
		}
		b, _ := json.Marshal(vals[0])
		fields[k] = b
	}
	decodeInto(reflect.ValueOf(&out).Elem(), fields, "", v)
	if vv, ok := any(&out).(Validatable); ok {
		vv.Validate(v)
	}
	return out, v.Err()
}
