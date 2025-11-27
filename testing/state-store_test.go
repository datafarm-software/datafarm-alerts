package testing

import (
	"testing"

	"github.com/geraud22/sensordata-alerts/alertsource"
)

type NotifyUser interface{}

type App struct {
	alerter  alertsource.AlertSource
	notifier NotifyUser
}

func (a *App) NotifyCrit(newLevel AlertLevel, oldLevel AlertLevel) bool {
	if newLevel == oldLevel {
		return false
	}
	if oldLevel < newLevel && newLevel == Crit {
		return true
	}
	return false
}

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
