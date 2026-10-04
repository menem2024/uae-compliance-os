package trackc

import "testing"

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !c.FixAgentAuto || c.ReadPerMinute != 600 || c.WritePerMinute != 120 || c.ExportsBucket != "documents" {
		t.Errorf("defaults = %+v", c)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{
		"FIX_AGENT_AUTO": "false", "C_READ_RATE_LIMIT_PER_MINUTE": "10", "C_WRITE_RATE_LIMIT_PER_MINUTE": "5", "EXPORTS_BUCKET": "exports-b",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if c.FixAgentAuto || c.ReadPerMinute != 10 || c.WritePerMinute != 5 || c.ExportsBucket != "exports-b" {
		t.Errorf("config = %+v", c)
	}
}

func TestLoadConfigRejectsBadValues(t *testing.T) {
	for k, v := range map[string]string{
		"FIX_AGENT_AUTO": "maybe", "C_READ_RATE_LIMIT_PER_MINUTE": "0", "C_WRITE_RATE_LIMIT_PER_MINUTE": "abc",
	} {
		if _, err := LoadConfig(env(map[string]string{k: v})); err == nil {
			t.Errorf("%s=%q accepted", k, v)
		}
	}
}
