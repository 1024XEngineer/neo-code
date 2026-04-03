package context

import "neo-code/internal/config"

func testMetadata(workdir string) Metadata {
	cfg := config.DefaultConfig()
	return Metadata{
		Workdir:  workdir,
		Shell:    cfg.Shell,
		Provider: cfg.SelectedProvider,
		Model:    cfg.CurrentModel,
	}
}
