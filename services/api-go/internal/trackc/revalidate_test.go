package trackc

import (
	"context"
	"testing"
)

func TestRevalidateRejectsBadOptions(t *testing.T) {
	for name, o := range map[string]RevalidateOpts{
		"no ruleset": {Rate: 1},
		"zero rate":  {Ruleset: "pint-ae@1.0.4+r1", Rate: 0},
	} {
		if _, err := Revalidate(context.Background(), o); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
