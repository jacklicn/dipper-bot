package lcm

// Config holds LCM (Lossless Context Management) settings.
type Config struct {
	Enabled               bool    `json:"enabled"`
	DatabasePath          string  `json:"databasePath"`
	ContextThreshold      float64 `json:"contextThreshold"`      // 0.0–1.0, trigger compaction when context reaches this fraction
	FreshTailCount        int     `json:"freshTailCount"`        // messages protected from compaction
	LeafMinFanout         int     `json:"leafMinFanout"`         // min raw messages per leaf summary
	CondensedMinFanout    int     `json:"condensedMinFanout"`    // min summaries per condensed node
	LeafChunkTokens       int     `json:"leafChunkTokens"`       // max source tokens per leaf chunk
	LeafTargetTokens      int     `json:"leafTargetTokens"`      // target tokens for leaf summaries
	CondensedTargetTokens int     `json:"condensedTargetTokens"` // target tokens for condensed summaries
	IncrementalMaxDepth   int     `json:"incrementalMaxDepth"`   // 0=leaf only, -1=unlimited
}

// DefaultConfig returns recommended LCM defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:               false,
		DatabasePath:          "",
		ContextThreshold:      0.75,
		FreshTailCount:        32,
		LeafMinFanout:         8,
		CondensedMinFanout:     4,
		LeafChunkTokens:       20000,
		LeafTargetTokens:      1200,
		CondensedTargetTokens: 2000,
		IncrementalMaxDepth:   -1,
	}
}
