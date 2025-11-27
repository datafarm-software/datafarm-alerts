package testing

import (
	"testing"
)

var app = &App{}

func TestNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want                  bool
		newLevel, storedLevel AlertLevel
	}{
		"no change so do nothing": {
			want:        false,
			newLevel:    Crit,
			storedLevel: Crit,
		},
		"change to crit, so notify": {
			want:        true,
			newLevel:    Crit,
			storedLevel: Ok,
		},
		"change from crit to ok": {
			want:        false,
			newLevel:    Ok,
			storedLevel: Crit,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got = app.NotifyCrit(tt.newLevel, tt.storedLevel)
			if got != tt.want {
				t.Fatalf("tt.want = %v, got = %v", tt.want, got)
			}
		})
	}
}
