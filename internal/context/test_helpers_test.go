package context

import "neo-code/internal/provider/builtin"

func testMetadata(workdir string) Metadata {
	cfg := builtin.DefaultConfig()
	return Metadata{
		Workdir:  workdir,
		Shell:    cfg.Shell,
		Provider: cfg.SelectedProvider,
		Model:    cfg.CurrentModel,
	}
}
