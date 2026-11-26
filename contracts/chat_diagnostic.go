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
			for _, child := range chatDiagnosticBranches(e) {
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
		if _, declared := chatFieldNames[part]; declared {
			path.WriteString(part)
		} else {
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

var chatFieldNames = func() map[string]struct{} {
	data, _ := files.ReadFile("schemas/chat.json")
	var schema any
	_ = json.Unmarshal(data, &schema)
	names := make(map[string]struct{})
	var walk func(any)
	walk = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if properties, ok := node["properties"].(map[string]any); ok {
				for name := range properties {
					names[name] = struct{}{}
				}
			}
			for _, child := range node {
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	walk(schema)
	return names
}()

// Ignore incompatible oneOf alternatives when a branch matches the object's
// discriminator and failed for another reason.
func chatDiagnosticBranches(e *jsonschema.ValidationError) []*jsonschema.ValidationError {
	if _, ok := e.ErrorKind.(*kind.OneOf); !ok {
		return e.Causes
	}
	var compatible []*jsonschema.ValidationError
	for _, branch := range e.Causes {
		if !chatBranchMismatch(branch, e.InstanceLocation) {
			compatible = append(compatible, branch)
		}
	}
	if len(compatible) != 0 {
		return compatible
	}
	return e.Causes
}

func chatBranchMismatch(e *jsonschema.ValidationError, location []string) bool {
	switch e.ErrorKind.(type) {
	case *kind.Type:
		return len(e.InstanceLocation) == len(location)
	case *kind.Const:
		return len(e.InstanceLocation) == len(location)+1 &&
			e.InstanceLocation[len(location)] == "type"
	case *kind.OneOf:
		return false
	}
	for _, child := range e.Causes {
		if chatBranchMismatch(child, location) {
			return true
		}
	}
	return false
}
