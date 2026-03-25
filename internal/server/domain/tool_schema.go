package domain

type ToolSchema struct {
	Type     string             `json:"type"`
	Function ToolFunctionSchema `json:"function"`
}

type ToolFunctionSchema struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Parameters  ToolParametersSchema `json:"parameters"`
}

type ToolParametersSchema struct {
	Type                 string                     `json:"type"`
	Properties           map[string]ToolParamSchema `json:"properties,omitempty"`
	Required             []string                   `json:"required,omitempty"`
	AdditionalProperties bool                       `json:"additionalProperties,omitempty"`
}

type ToolParamSchema struct {
	Type        string           `json:"type,omitempty"`
	Description string           `json:"description,omitempty"`
	Enum        []string         `json:"enum,omitempty"`
	Default     interface{}      `json:"default,omitempty"`
	Items       *ToolParamSchema `json:"items,omitempty"`
}
