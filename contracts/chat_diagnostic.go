package contracts

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

type ChatViolation struct {
	Path     string `json:"path"`
	Rule     string `json:"rule"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// ChatSchemaViolation projects structured validator errors without copying values
// or unknown property names from the request.
func ChatSchemaViolation(input json.RawMessage) *ChatViolation {
	once.Do(initSchemas)
	value, err := Decode(input)
	if err != nil {
		return nil
	}
	failure, ok := validators["chat"].Validate(value).(*jsonschema.ValidationError)
	if !ok {
		return nil
	}
	var best *ChatViolation
	bestScore := -1
	var visit func(*jsonschema.ValidationError)
	visit = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, child := range e.Causes {
				visit(child)
			}
			return
		}
		location := append([]string(nil), e.InstanceLocation...)
		d := ChatViolation{}
		priority := 0
		switch k := e.ErrorKind.(type) {
		case *kind.Type:
			priority = 3
			d.Rule, d.Expected, d.Actual = "type", strings.Join(k.Want, " or "), k.Got
		case *kind.Const:
			priority = 2
			d.Rule, d.Expected, d.Actual = "const", "schema discriminator", chatJSONType(k.Got)
		case *kind.Enum:
			priority = 2
			d.Rule, d.Expected, d.Actual = "enum", "allowed value", chatJSONType(k.Got)
		case *kind.Required:
			priority = 1
			if len(k.Missing) == 0 {
				return
			}
			location = append(location, k.Missing[0])
			d.Rule, d.Expected, d.Actual = "required", "present", "missing"
		case *kind.AdditionalProperties:
			location = append(location, "unknown_field")
			d.Rule, d.Expected = "additionalProperties", "declared property"
		default:
			path := e.ErrorKind.KeywordPath()
			if len(path) == 0 {
				return
			}
			d.Rule = path[0]
			d.Expected = "schema constraint"
		}
		score := len(location)*4 + priority
		if score <= bestScore || len(location) > 32 {
			return
		}
		d.Path = safeChatPath(value, location)
		best, bestScore = &d, score
	}
	visit(failure)
	return best
}

func safeChatPath(value any, parts []string) string {
	var path strings.Builder
	for _, part := range parts {
		if list, ok := value.([]any); ok {
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(list) {
				break
			}
			path.WriteString("[" + strconv.Itoa(index) + "]")
			value = list[index]
			continue
		}
		if path.Len() > 0 {
			path.WriteByte('.')
		}
		switch part {
		case "model", "messages", "role", "content", "type", "text", "image_url", "url", "detail", "name", "tool_calls", "tool_call_id", "tools", "function", "arguments", "parameters", "description", "strict", "id", "stream", "max_tokens", "max_completion_tokens", "response_format", "json_schema", "schema", "tool_choice", "stream_options":
			path.WriteString(part)
		default:
			path.WriteString("unknown_field")
		}
		object, _ := value.(map[string]any)
		value = object[part]
	}
	if path.Len() == 0 {
		return "$"
	}
	return path.String()
}

func chatJSONType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case json.Number, float64:
		return "number"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return ""
}
