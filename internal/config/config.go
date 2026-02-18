package config

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var Logger = newLogger()

func newLogger() *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "DEBUG":
		level.Set(slog.LevelDebug)
	case "WARN":
		level.Set(slog.LevelWarn)
	case "ERROR":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

type Account struct {
	Email    string `json:"email,omitempty"`
	Mobile   string `json:"mobile,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"`
}

func (a Account) Identifier() string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if strings.TrimSpace(a.Mobile) != "" {
		return strings.TrimSpace(a.Mobile)
	}
	// Backward compatibility: old configs may contain token-only accounts.
	// Use a stable non-sensitive synthetic id so they can still join the pool.
	token := strings.TrimSpace(a.Token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "token:" + hex.EncodeToString(sum[:8])
}

type Config struct {
	Keys             []string          `json:"keys,omitempty"`
	Accounts         []Account         `json:"accounts,omitempty"`
	ClaudeMapping    map[string]string `json:"claude_mapping,omitempty"`
	ClaudeModelMap   map[string]string `json:"claude_model_mapping,omitempty"`
	ModelAliases     map[string]string `json:"model_aliases,omitempty"`
	Compat           CompatConfig      `json:"compat,omitempty"`
	Toolcall         ToolcallConfig    `json:"toolcall,omitempty"`
	Responses        ResponsesConfig   `json:"responses,omitempty"`
	Embeddings       EmbeddingsConfig  `json:"embeddings,omitempty"`
	VercelSyncHash   string            `json:"_vercel_sync_hash,omitempty"`
	VercelSyncTime   int64             `json:"_vercel_sync_time,omitempty"`
	AdditionalFields map[string]any    `json:"-"`
}

type CompatConfig struct {
	WideInputStrictOutput bool `json:"wide_input_strict_output,omitempty"`
}

type ToolcallConfig struct {
	Mode                string `json:"mode,omitempty"`
	EarlyEmitConfidence string `json:"early_emit_confidence,omitempty"`
}

type ResponsesConfig struct {
	StoreTTLSeconds int `json:"store_ttl_seconds,omitempty"`
}

type EmbeddingsConfig struct {
	Provider string `json:"provider,omitempty"`
}

func (c Config) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	for k, v := range c.AdditionalFields {
		m[k] = v
	}
	if len(c.Keys) > 0 {
		m["keys"] = c.Keys
	}
	if len(c.Accounts) > 0 {
		m["accounts"] = c.Accounts
	}
	if len(c.ClaudeMapping) > 0 {
		m["claude_mapping"] = c.ClaudeMapping
	}
	if len(c.ClaudeModelMap) > 0 {
		m["claude_model_mapping"] = c.ClaudeModelMap
	}
	if len(c.ModelAliases) > 0 {
		m["model_aliases"] = c.ModelAliases
	}
	if c.Compat.WideInputStrictOutput {
		m["compat"] = c.Compat
	}
	if strings.TrimSpace(c.Toolcall.Mode) != "" || strings.TrimSpace(c.Toolcall.EarlyEmitConfidence) != "" {
		m["toolcall"] = c.Toolcall
	}
	if c.Responses.StoreTTLSeconds > 0 {
		m["responses"] = c.Responses
	}
	if strings.TrimSpace(c.Embeddings.Provider) != "" {
		m["embeddings"] = c.Embeddings
	}
	if c.VercelSyncHash != "" {
		m["_vercel_sync_hash"] = c.VercelSyncHash
	}
	if c.VercelSyncTime != 0 {
		m["_vercel_sync_time"] = c.VercelSyncTime
	}
	return json.Marshal(m)
}

func (c *Config) UnmarshalJSON(b []byte) error {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	c.AdditionalFields = map[string]any{}
	for k, v := range raw {
		switch k {
		case "keys":
			if err := json.Unmarshal(v, &c.Keys); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "accounts":
			if err := json.Unmarshal(v, &c.Accounts); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "claude_mapping":
			if err := json.Unmarshal(v, &c.ClaudeMapping); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "claude_model_mapping":
			if err := json.Unmarshal(v, &c.ClaudeModelMap); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "model_aliases":
			if err := json.Unmarshal(v, &c.ModelAliases); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "compat":
			if err := json.Unmarshal(v, &c.Compat); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "toolcall":
			if err := json.Unmarshal(v, &c.Toolcall); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "responses":
			if err := json.Unmarshal(v, &c.Responses); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "embeddings":
			if err := json.Unmarshal(v, &c.Embeddings); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "_vercel_sync_hash":
			if err := json.Unmarshal(v, &c.VercelSyncHash); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		case "_vercel_sync_time":
			if err := json.Unmarshal(v, &c.VercelSyncTime); err != nil {
				return fmt.Errorf("invalid field %q: %w", k, err)
			}
		default:
			var anyVal any
			if err := json.Unmarshal(v, &anyVal); err == nil {
				c.AdditionalFields[k] = anyVal
			}
		}
	}
	return nil
}

func (c Config) Clone() Config {
	clone := Config{
		Keys:             slices.Clone(c.Keys),
		Accounts:         slices.Clone(c.Accounts),
		ClaudeMapping:    cloneStringMap(c.ClaudeMapping),
		ClaudeModelMap:   cloneStringMap(c.ClaudeModelMap),
		ModelAliases:     cloneStringMap(c.ModelAliases),
		Compat:           c.Compat,
		Toolcall:         c.Toolcall,
		Responses:        c.Responses,
		Embeddings:       c.Embeddings,
		VercelSyncHash:   c.VercelSyncHash,
		VercelSyncTime:   c.VercelSyncTime,
		AdditionalFields: map[string]any{},
	}
	for k, v := range c.AdditionalFields {
		clone.AdditionalFields[k] = v
	}
	return clone
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type Store struct {
	mu      sync.RWMutex
	cfg     Config
	path    string
	fromEnv bool
	keyMap  map[string]struct{} // O(1) API key lookup index
	accMap  map[string]int      // O(1) account lookup: identifier -> slice index
}

func BaseDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func IsVercel() bool {
	return strings.TrimSpace(os.Getenv("VERCEL")) != "" || strings.TrimSpace(os.Getenv("NOW_REGION")) != ""
}

func ResolvePath(envKey, defaultRel string) string {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw != "" {
		if filepath.IsAbs(raw) {
			return raw
		}
		return filepath.Join(BaseDir(), raw)
	}
	return filepath.Join(BaseDir(), defaultRel)
}

func ConfigPath() string {
	return ResolvePath("DS2API_CONFIG_PATH", "config.json")
}

func WASMPath() string {
	return ResolvePath("DS2API_WASM_PATH", "sha3_wasm_bg.7b9ca65ddd.wasm")
}

func StaticAdminDir() string {
	return ResolvePath("DS2API_STATIC_ADMIN_DIR", "static/admin")
}

func LoadStore() *Store {
	cfg, fromEnv, err := loadConfig()
	if err != nil {
		Logger.Warn("[config] load failed", "error", err)
	}
	if len(cfg.Keys) == 0 && len(cfg.Accounts) == 0 {
		Logger.Warn("[config] empty config loaded")
	}
	s := &Store{cfg: cfg, path: ConfigPath(), fromEnv: fromEnv}
	s.rebuildIndexes()
	return s
}

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

func loadConfig() (Config, bool, error) {
	rawCfg := strings.TrimSpace(os.Getenv("DS2API_CONFIG_JSON"))
	if rawCfg == "" {
		rawCfg = strings.TrimSpace(os.Getenv("CONFIG_JSON"))
	}
	if rawCfg != "" {
		cfg, err := parseConfigString(rawCfg)
		return cfg, true, err
	}

	content, err := os.ReadFile(ConfigPath())
	if err != nil {
		if IsVercel() {
			// Vercel one-click deploy may start without a writable/present config file.
			// Keep an in-memory config so users can bootstrap via WebUI then sync env.
			return Config{}, true, nil
		}
		return Config{}, false, err
	}
	var cfg Config
	if err := json.Unmarshal(content, &cfg); err != nil {
		return Config{}, false, err
	}
	if IsVercel() {
		// Vercel filesystem is ephemeral/read-only for runtime writes; avoid save errors.
		return cfg, true, nil
	}
	return cfg, false, nil
}

func parseConfigString(raw string) (Config, error) {
	var cfg Config
	candidates := []string{raw}
	if normalized := normalizeConfigInput(raw); normalized != raw {
		candidates = append(candidates, normalized)
	}
	for _, candidate := range candidates {
		if err := json.Unmarshal([]byte(candidate), &cfg); err == nil {
			return cfg, nil
		}
	}

	base64Input := candidates[len(candidates)-1]
	decoded, err := decodeConfigBase64(base64Input)
	if err != nil {
		return Config{}, fmt.Errorf("invalid DS2API_CONFIG_JSON: %w", err)
	}
	if err := json.Unmarshal(decoded, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid DS2API_CONFIG_JSON decoded JSON: %w", err)
	}
	return cfg, nil
}

func normalizeConfigInput(raw string) string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return normalized
	}
	for {
		changed := false
		if len(normalized) >= 2 {
			first := normalized[0]
			last := normalized[len(normalized)-1]
			if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
				normalized = strings.TrimSpace(normalized[1 : len(normalized)-1])
				changed = true
			}
		}
		if strings.HasPrefix(strings.ToLower(normalized), "base64:") {
			normalized = strings.TrimSpace(normalized[len("base64:"):])
			changed = true
		}
		if !changed {
			break
		}
	}
	return strings.TrimSpace(normalized)
}

func decodeConfigBase64(raw string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encodings {
		decoded, err := enc.DecodeString(raw)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("base64 decode failed")
}

func (s *Store) Snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Clone()
}

func (s *Store) HasAPIKey(k string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.keyMap[k]
	return ok
}

func (s *Store) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.cfg.Keys)
}

func (s *Store) Accounts() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Clone(s.cfg.Accounts)
}

func (s *Store) FindAccount(identifier string) (Account, bool) {
	identifier = strings.TrimSpace(identifier)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if idx, ok := s.findAccountIndexLocked(identifier); ok {
		return s.cfg.Accounts[idx], true
	}
	return Account{}, false
}

func (s *Store) UpdateAccountToken(identifier, token string) error {
	identifier = strings.TrimSpace(identifier)
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.findAccountIndexLocked(identifier)
	if !ok {
		return errors.New("account not found")
	}
	oldID := s.cfg.Accounts[idx].Identifier()
	s.cfg.Accounts[idx].Token = token
	newID := s.cfg.Accounts[idx].Identifier()
	// Keep historical aliases usable for long-lived queues while also adding
	// the latest identifier after token refresh.
	if identifier != "" {
		s.accMap[identifier] = idx
	}
	if oldID != "" {
		s.accMap[oldID] = idx
	}
	if newID != "" {
		s.accMap[newID] = idx
	}
	return s.saveLocked()
}

func (s *Store) Replace(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg.Clone()
	s.rebuildIndexes()
	return s.saveLocked()
}

func (s *Store) Update(mutator func(*Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.cfg.Clone()
	if err := mutator(&cfg); err != nil {
		return err
	}
	s.cfg = cfg
	s.rebuildIndexes()
	return s.saveLocked()
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fromEnv {
		Logger.Info("[save_config] source from env, skip write")
		return nil
	}
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}

func (s *Store) saveLocked() error {
	if s.fromEnv {
		Logger.Info("[save_config] source from env, skip write")
		return nil
	}
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
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

func (s *Store) IsEnvBacked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fromEnv
}

func (s *Store) SetVercelSync(hash string, ts int64) error {
	return s.Update(func(c *Config) error {
		c.VercelSyncHash = hash
		c.VercelSyncTime = ts
		return nil
	})
}

func (s *Store) ExportJSONAndBase64() (string, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, err := json.Marshal(s.cfg)
	if err != nil {
		return "", "", err
	}
	return string(b), base64.StdEncoding.EncodeToString(b), nil
}

func (s *Store) ClaudeMapping() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.cfg.ClaudeModelMap) > 0 {
		return cloneStringMap(s.cfg.ClaudeModelMap)
	}
	if len(s.cfg.ClaudeMapping) > 0 {
		return cloneStringMap(s.cfg.ClaudeMapping)
	}
	return map[string]string{"fast": "deepseek-chat", "slow": "deepseek-reasoner"}
}

func (s *Store) ModelAliases() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := DefaultModelAliases()
	for k, v := range s.cfg.ModelAliases {
		key := strings.TrimSpace(lower(k))
		val := strings.TrimSpace(lower(v))
		if key == "" || val == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func (s *Store) CompatWideInputStrictOutput() bool {
	// Current default policy is always wide-input / strict-output.
	// Kept as a method so callers do not depend on storage shape.
	return true
}

func (s *Store) ToolcallMode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mode := strings.TrimSpace(strings.ToLower(s.cfg.Toolcall.Mode))
	if mode == "" {
		return "feature_match"
	}
	return mode
}

func (s *Store) ToolcallEarlyEmitConfidence() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	level := strings.TrimSpace(strings.ToLower(s.cfg.Toolcall.EarlyEmitConfidence))
	if level == "" {
		return "high"
	}
	return level
}

func (s *Store) ResponsesStoreTTLSeconds() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.Responses.StoreTTLSeconds > 0 {
		return s.cfg.Responses.StoreTTLSeconds
	}
	return 900
}

func (s *Store) EmbeddingsProvider() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.cfg.Embeddings.Provider)
}
