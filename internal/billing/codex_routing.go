package billing

import "maps"

// CodexRouting is plugin-owned configuration, not a mutation of CPA's auth files.
// Overrides use credential fingerprints; neither tokens nor account emails are stored.
type CodexRouting struct {
	Enabled bool              `json:"enabled"`
	Roles   map[string]string `json:"roles"`
}

func (s *Store) CodexRouting() CodexRouting {
	var config CodexRouting
	s.read(func(state *State) { config = state.CodexRouting; config.Roles = maps.Clone(config.Roles) })
	if config.Roles == nil {
		config.Roles = map[string]string{}
	}
	return config
}

func (s *Store) SetCodexRouting(config CodexRouting) error {
	if len(config.Roles) > 10000 {
		return invalidf("Too many Codex routing overrides")
	}
	for ref, role := range config.Roles {
		if !ValidCredentialFingerprint(ref) || (role != "primary" && role != "reserve") {
			return invalidf("Codex roles must use a credential fingerprint and primary or reserve")
		}
	}
	_, err := editConfiguration(s, func(state *State) (struct{}, Changes, error) {
		config.Roles = maps.Clone(config.Roles)
		state.CodexRouting = config
		return struct{}{}, Changes{CodexRouting: true}, nil
	})
	return err
}
