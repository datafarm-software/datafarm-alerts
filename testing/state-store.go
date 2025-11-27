package testing

import "testing"

type AlertLevel int

const (
	Ok AlertLevel = iota
	Warn
	Crit
)

type AlertSource interface{}
type NotifyUser interface{}

type Mastermind struct {
	alerter  AlertSource
	notifier NotifyUser
}

var app = &Mastermind{}

func TestRespondToLevel(t *testing.T) {
	var err error
	var got bool
	tests := map[string]struct {
		wantErr, want         bool
		newLevel, storedLevel AlertLevel
	}{}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err = app.RespondToLevel(tt.newLevel, tt.storedLevel)
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr: %v, gotErr: %v", tt.wantErr, err)
			}
			if got != tt.want {
				t.Fatalf("tt.want = %v, got = %v", tt.want, got)
			}
		})
	}
}
