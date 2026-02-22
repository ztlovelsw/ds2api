package config

// rebuildIndexes must be called with the lock already held (or during init).
func (s *Store) rebuildIndexes() {
	s.keyMap = make(map[string]struct{}, len(s.cfg.Keys))
	for _, k := range s.cfg.Keys {
		s.keyMap[k] = struct{}{}
	}
	s.accMap = make(map[string]int, len(s.cfg.Accounts))
	for i, acc := range s.cfg.Accounts {
		id := acc.Identifier()
		if id != "" {
			s.accMap[id] = i
		}
	}
}

// findAccountIndexLocked expects the store lock to already be held.
func (s *Store) findAccountIndexLocked(identifier string) (int, bool) {
	if idx, ok := s.accMap[identifier]; ok && idx >= 0 && idx < len(s.cfg.Accounts) {
		return idx, true
	}
	// Fallback for token-only accounts whose derived identifier changed after
	// a token refresh; this preserves correctness on map misses.
	for i, acc := range s.cfg.Accounts {
		if acc.Identifier() == identifier {
			return i, true
		}
	}
	return -1, false
}
