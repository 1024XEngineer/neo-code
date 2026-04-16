package services

import (
	"strings"

	providertypes "neo-code/internal/provider/types"
)

func renderPartsForTest(parts []providertypes.ContentPart) string {
	var builder strings.Builder
	for _, part := range parts {
		switch part.Kind {
		case providertypes.ContentPartText:
			builder.WriteString(part.Text)
		case providertypes.ContentPartImage:
			builder.WriteString("[Image]")
		}
	}
	return builder.String()
}
