package billing

import "maps"

// CodexRouting is plugin-owned configuration, not a mutation of CPA's auth files.
// Overrides use credential fingerprints; neither tokens nor account emails are stored.
type CodexRouting struct {
	Enabled   bool              `json:"enabled"`
	Roles     map[string]string `json:"roles"`
	AutoStart map[string]bool   `json:"auto_start"`
}

func (s *Store) CodexRouting() CodexRouting {
	var config CodexRouting
	s.read(func(state *State) {
		config = state.CodexRouting
		config.Roles = maps.Clone(config.Roles)
		config.AutoStart = maps.Clone(config.AutoStart)
	})
	if config.Roles == nil {
		config.Roles = map[string]string{}
	}
	if config.AutoStart == nil {
		config.AutoStart = map[string]bool{}
	}
	return config
}

func (s *Store) SetCodexRouting(config CodexRouting) error {
	if len(config.Roles) > 10000 || len(config.AutoStart) > 10000 {
		return invalidf("Too many Codex routing overrides")
	}
	config.AutoStart = maps.Clone(config.AutoStart)
	for ref, role := range config.Roles {
		if !ValidCredentialFingerprint(ref) || (role != "primary" && role != "reserve") {
			return invalidf("Codex roles must use a credential fingerprint and primary or reserve")
		}
	}
	for ref, enabled := range config.AutoStart {
		if !ValidCredentialFingerprint(ref) {
			return invalidf("Codex window auto-start must use credential fingerprints")
		}
		if !enabled {
			delete(config.AutoStart, ref)
		}
	}
	_, err := editConfiguration(s, func(state *State) (struct{}, Changes, error) {
		config.Roles = maps.Clone(config.Roles)
		config.AutoStart = maps.Clone(config.AutoStart)
		state.CodexRouting = config
		return struct{}{}, Changes{CodexRouting: true}, nil
	})
	return err
}
