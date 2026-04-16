package types

import "strings"

func renderPartsForTest(parts []ContentPart) string {
	var builder strings.Builder
	for _, part := range parts {
		switch part.Kind {
		case ContentPartText:
			builder.WriteString(part.Text)
		case ContentPartImage:
			builder.WriteString("[Image]")
		}
	}
	return builder.String()
}
