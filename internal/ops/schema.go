package ops

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Schema 参数说明：JSON Schema 的一个子集（两家 AI 的工具调用都认这种格式，可以直接编成 JSON 交给它们）。
// 校验只认这里列出的几种约束；值为 null 的参数按「没传」处理。
type Schema struct {
	Type        string             `json:"type"` // object / string / integer / number / boolean / array
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Minimum     *float64           `json:"minimum,omitempty"`
	Maximum     *float64           `json:"maximum,omitempty"`
	MinLength   *int               `json:"minLength,omitempty"`
	MaxLength   *int               `json:"maxLength,omitempty"`
	MaxItems    *int               `json:"maxItems,omitempty"`
	Pattern     string             `json:"pattern,omitempty"`

	re *regexp.Regexp
}

// Object 对象；required 是必填的参数名。
func Object(props map[string]*Schema, required ...string) *Schema {
	return &Schema{Type: "object", Properties: props, Required: required}
}

func String(desc string) *Schema  { return &Schema{Type: "string", Description: desc} }
func Integer(desc string) *Schema { return &Schema{Type: "integer", Description: desc} }
func Number(desc string) *Schema  { return &Schema{Type: "number", Description: desc} }
func Boolean(desc string) *Schema { return &Schema{Type: "boolean", Description: desc} }

// Array 数组，items 是每一项的说明。
func Array(desc string, items *Schema) *Schema {
	return &Schema{Type: "array", Description: desc, Items: items}
}

// Len 限定字符串长度（按字符数算）。
func (s *Schema) Len(min, max int) *Schema { s.MinLength, s.MaxLength = &min, &max; return s }

// Range 限定数值范围。
func (s *Schema) Range(min, max float64) *Schema { s.Minimum, s.Maximum = &min, &max; return s }

// OneOf 限定只能取这几个值。
func (s *Schema) OneOf(vals ...string) *Schema { s.Enum = vals; return s }

// Match 限定字符串必须整个匹配这个正则。
func (s *Schema) Match(pattern string) *Schema { s.Pattern = pattern; return s }

// MaxLen 限定数组最多几项。
func (s *Schema) MaxLen(n int) *Schema { s.MaxItems = &n; return s }

// compile 预编译正则（Register 时调用），写错了直接 panic。
func (s *Schema) compile() {
	if s == nil {
		return
	}
	if s.Pattern != "" && s.re == nil {
		s.re = regexp.MustCompile(`^(?:` + s.Pattern + `)$`)
	}
	for _, p := range s.Properties {
		p.compile()
	}
	s.Items.compile()
}

var typeNames = map[string]string{
	"object": "对象", "string": "字符串", "integer": "整数", "number": "数字", "boolean": "布尔值", "array": "数组",
}

// name 句中的参数名：根对象叫「参数」，其余写成「参数 xmxMb 」（西文前后留空格）。
func name(path string) string {
	if path == "" {
		return "参数"
	}
	return "参数 " + path + " "
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// validate 校验 v（json.Decoder 开了 UseNumber 解出来的值）。
func (s *Schema) validate(path string, v any) error {
	bad := func() error { return fmt.Errorf("%s应为%s", name(path), typeNames[s.Type]) }
	switch s.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		for _, req := range s.Required {
			if obj[req] == nil {
				return fmt.Errorf("缺少参数 %s", join(path, req))
			}
		}
		keys := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			keys = append(keys, k)
		}
		slices.Sort(keys) // 报错顺序固定
		for _, k := range keys {
			if val, ok := obj[k]; ok && val != nil {
				if err := s.Properties[k].validate(join(path, k), val); err != nil {
					return err
				}
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return bad()
		}
		if s.MaxItems != nil && len(arr) > *s.MaxItems {
			return fmt.Errorf("%s最多 %d 项", name(path), *s.MaxItems)
		}
		if s.Items != nil {
			for i, it := range arr {
				if it == nil {
					return fmt.Errorf("%s不能为 null", name(fmt.Sprintf("%s[%d]", path, i)))
				}
				if err := s.Items.validate(fmt.Sprintf("%s[%d]", path, i), it); err != nil {
					return err
				}
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return bad()
		}
		n := utf8.RuneCountInString(str)
		if (s.MinLength != nil && n < *s.MinLength) || (s.MaxLength != nil && n > *s.MaxLength) {
			lo, hi := 0, -1
			if s.MinLength != nil {
				lo = *s.MinLength
			}
			if s.MaxLength != nil {
				hi = *s.MaxLength
			}
			switch {
			case hi < 0:
				return fmt.Errorf("%s至少 %d 个字", name(path), lo)
			case lo == 0:
				return fmt.Errorf("%s最多 %d 个字", name(path), hi)
			}
			return fmt.Errorf("%s应为 %d 到 %d 个字", name(path), lo, hi)
		}
		if len(s.Enum) > 0 && !slices.Contains(s.Enum, str) {
			return fmt.Errorf("%s只能是 %s 之一", name(path), strings.Join(s.Enum, "、"))
		}
		if s.re != nil && !s.re.MatchString(str) {
			return fmt.Errorf("%s格式不对", name(path))
		}
	case "integer", "number":
		n, ok := v.(json.Number)
		if !ok {
			return bad()
		}
		var f float64
		if s.Type == "integer" {
			i, err := strconv.ParseInt(string(n), 10, 64)
			if err != nil {
				return bad()
			}
			f = float64(i)
		} else {
			var err error
			if f, err = n.Float64(); err != nil {
				return bad()
			}
		}
		if (s.Minimum != nil && f < *s.Minimum) || (s.Maximum != nil && f > *s.Maximum) {
			switch {
			case s.Maximum == nil:
				return fmt.Errorf("%s不能小于 %s", name(path), num(*s.Minimum))
			case s.Minimum == nil:
				return fmt.Errorf("%s不能大于 %s", name(path), num(*s.Maximum))
			}
			return fmt.Errorf("%s应在 %s 到 %s 之间", name(path), num(*s.Minimum), num(*s.Maximum))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return bad()
		}
	}
	return nil
}

func join(path, k string) string {
	if path == "" {
		return k
	}
	return path + "." + k
}
