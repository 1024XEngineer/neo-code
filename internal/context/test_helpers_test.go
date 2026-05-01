package context

import "neo-code/internal/config"

func testMetadata(workdir string) Metadata {
	cfg := config.StaticDefaults()
	providers := config.DefaultProviders()
	providerName := ""
	modelID := ""
	if len(providers) > 0 {
		providerName = providers[0].Name
		modelID = providers[0].Model
	}
	return Metadata{
		ProjectRoot: workdir,
		Workdir:     workdir,
		Shell:       cfg.Shell,
		Provider:    providerName,
		Model:       modelID,
	}
}
