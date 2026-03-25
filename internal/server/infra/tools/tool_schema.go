package tools

import "go-llm-demo/internal/server/domain"

func BuildToolSchemas(defs []domain.ToolDefinition) []domain.ToolSchema {
	if len(defs) == 0 {
		return nil
	}
	schemas := make([]domain.ToolSchema, 0, len(defs))
	for _, def := range defs {
		schemas = append(schemas, buildToolSchema(def))
	}
	return schemas
}

func buildToolSchema(def domain.ToolDefinition) domain.ToolSchema {
	properties := make(map[string]domain.ToolParamSchema, len(def.Parameters))
	required := make([]string, 0, len(def.Parameters))

	for _, param := range def.Parameters {
		properties[param.Name] = domain.ToolParamSchema{
			Type:        mapParamType(param.Type),
			Description: param.Description,
			Enum:        param.Enum,
			Default:     param.DefaultValue,
		}
		if param.Required {
			required = append(required, param.Name)
		}
	}

	return domain.ToolSchema{
		Type: "function",
		Function: domain.ToolFunctionSchema{
			Name:        def.Name,
			Description: def.Description,
			Parameters: domain.ToolParametersSchema{
				Type:                 "object",
				Properties:           properties,
				Required:             required,
				AdditionalProperties: false,
			},
		},
	}
}

func mapParamType(paramType string) string {
	switch paramType {
	case "string", "integer", "number", "boolean", "object", "array":
		return paramType
	default:
		return "string"
	}
}
