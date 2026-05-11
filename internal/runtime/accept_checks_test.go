package runtime

import agentsession "neo-code/internal/session"

func acceptText(items ...string) agentsession.AcceptChecks {
	out := make(agentsession.AcceptChecks, 0, len(items))
	for _, item := range items {
		out = append(out, agentsession.AcceptCheck{Kind: agentsession.AcceptCheckOutputOnly, Target: item})
	}
	return out
}
