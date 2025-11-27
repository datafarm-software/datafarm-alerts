package testing

import (
	"testing"
	"time"

	"github.com/geraud22/sensordata-alerts/alertsource"
	"github.com/geraud22/sensordata-alerts/app"
)

var a = &app.App{}

func TestNotifyCrit(t *testing.T) {
	var got bool
	tests := map[string]struct {
		want            bool
		alert, previous *alertsource.AlertState
	}{
		"no change, within duration, so do nothing": {
			want: false,
			alert: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now(),
				Duration: time.Duration(24 * time.Hour),
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now().Add(-(6 * time.Hour)),
				Duration: time.Duration(24 * time.Hour),
			},
		},
		"still crit after duration, so notify": {
			want: true,
			alert: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now(),
				Duration: time.Duration(24 * time.Hour),
			},
			previous: &alertsource.AlertState{
				Level:    alertsource.Crit,
				Time:     time.Now().Add(-(25 * time.Hour)),
				Duration: time.Duration(24 * time.Hour),
			},
		},
		"change to crit, so notify": {
			want: true,
			alert: &alertsource.AlertState{
				Level: alertsource.Crit,
			},
			previous: &alertsource.AlertState{
				Level: alertsource.Ok,
			},
		},
		"change from crit to ok": {
			want: false,
			alert: &alertsource.AlertState{
				Level: alertsource.Ok,
			},
			previous: &alertsource.AlertState{
				Level: alertsource.Crit,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got = a.ShouldNotifyCrit(tt.alert, tt.previous)
			if got != tt.want {
				t.Fatalf("tt.want = %v, got = %v", tt.want, got)
			}
		})
	}
}
