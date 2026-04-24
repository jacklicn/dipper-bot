package tools

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// CoerceStringParam turns LLM tool arguments into a string when the schema expects "string".
// Some providers (e.g. Gemini) may pass nested maps/slices instead of a plain string.
func CoerceStringParam(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		s := string(b)
		// If the model sent a JSON string value encoded twice, unwrap one level.
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			var inner string
			if json.Unmarshal([]byte(s), &inner) == nil {
				return inner
			}
		}
		return s
	}
}
