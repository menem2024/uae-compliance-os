package trackc

import (
	"fmt"
	"strconv"
	"strings"
)

// Config holds the Track C settings. They are read here and not in internal/config, which Track B
// edits for its own variables.
type Config struct {
	FixAgentAuto   bool   // FIX_AGENT_AUTO, default true
	ReadPerMinute  int64  // C_READ_RATE_LIMIT_PER_MINUTE, default 600 (GET routes, limiter c-read)
	WritePerMinute int64  // C_WRITE_RATE_LIMIT_PER_MINUTE, default 120 (mutations, limiter c-write)
	ExportsBucket  string // EXPORTS_BUCKET, default "documents"
}

// LoadConfig reads the Track C environment through getenv (os.Getenv in production). An unset
// variable takes its default; a set but invalid one is an error, so a typo never silently
// disables a limit.
func LoadConfig(getenv func(string) string) (Config, error) {
	c := Config{FixAgentAuto: true, ReadPerMinute: 600, WritePerMinute: 120, ExportsBucket: "documents"}
	if v := strings.TrimSpace(getenv("FIX_AGENT_AUTO")); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("FIX_AGENT_AUTO=%q: want true or false", v)
		}
		c.FixAgentAuto = b
	}
	for _, p := range []struct {
		key string
		dst *int64
	}{
		{"C_READ_RATE_LIMIT_PER_MINUTE", &c.ReadPerMinute},
		{"C_WRITE_RATE_LIMIT_PER_MINUTE", &c.WritePerMinute},
	} {
		if v := strings.TrimSpace(getenv(p.key)); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				return Config{}, fmt.Errorf("%s=%q: want a positive integer", p.key, v)
			}
			*p.dst = n
		}
	}
	if v := strings.TrimSpace(getenv("EXPORTS_BUCKET")); v != "" {
		c.ExportsBucket = v
	}
	return c, nil
}
