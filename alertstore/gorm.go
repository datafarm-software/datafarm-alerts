package alertstore

import (
	"context"
	"fmt"
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var ctx = context.Background()

type Mode int

const (
	Testing = iota
	Production
)

type Gorm struct {
	db         *gorm.DB
	mode       Mode
	dbFilename string
}

func NewGorm(mode Mode, dbName string) (*Gorm, error) {
	dbFilename := fmt.Sprintf("%s.db", dbName)
	db, err := gorm.Open(sqlite.Open(dbFilename), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("opening db: %v", err)
	}
	return &Gorm{
		db:         db,
		mode:       mode,
		dbFilename: dbFilename,
	}, nil
}

func (g *Gorm) Close() error {
	var err error
	if g.mode == Testing {
		err = os.Remove(g.dbFilename)
	}
	return err
}

func (g *Gorm) PrepareDb(dbRecord Schema) error {
	var err error
	if err = g.db.AutoMigrate(&DeviceAlertRule{}); err != nil {
		return err
	}
	if err = g.db.AutoMigrate(&AlertRecipient{}); err != nil {
		return err
	}
	if err = g.db.AutoMigrate(&AlertState{}); err != nil {
		return err
	}
	if err = g.db.AutoMigrate(&StoredAlertRule{}); err != nil {
		return err
	}
	return g.StoreAlertRule(dbRecord)
}

func (g *Gorm) GetSnapshot() *Schema {
	device_alert_rules, err := gorm.G[DeviceAlertRule](g.db).Find(ctx)
	if err != nil {
		return nil
	}
	alertRecipients, err := gorm.G[AlertRecipient](g.db).Find(ctx)
	if err != nil {
		return nil
	}
	stored_alert_rules, err := gorm.G[StoredAlertRule](g.db).Find(ctx)
	if err != nil {
		return nil
	}
	alertStates, err := gorm.G[AlertState](g.db).Find(ctx)
	if err != nil {
		return nil
	}
	return &Schema{
		DeviceAlerts:    device_alert_rules,
		AlertRecipients: alertRecipients,
		AlertStates:     alertStates,
		AlertRules:      stored_alert_rules,
	}
}

// NOTE: could return alertstore.NoAlert
func (g *Gorm) GetAlertState(alertId string) (AlertState, error) {
	return AlertState{}, fmt.Errorf("not implemented")
}
func (g *Gorm) GetAlertRule(alertId string) (AlertRule, error) {
	var ar AlertRule
	ar.AlertId = alertId

	var recipients []string
	db := g.db.Table("alert_recipients").
		Where("alert_id = ?", alertId).
		Select("email").
		Scan(&recipients)
	if db.Error != nil {
		return ar, fmt.Errorf("%w, detail: %v", NoAlert, db.Error)
	}
	if db.RowsAffected < 1 {
		return ar, NoAlert
	}
	ar.EmailAddresses = recipients

	db = g.db.Debug().Table("stored_alert_rules AS sar").
		Select(`
			sar.alert_id,
			dar.device_id,
			dar.query_field,
			sar.range_values,
			sar.duration,
			sar.alert_range,
			json_group_array(ar.email) AS email_addresses
		`).
		Joins(`
			LEFT JOIN device_alert_rules dar
			ON dar.alert_id = sar.alert_id
		`).
		Joins(`
			LEFT JOIN alert_recipients ar
			ON sar.alert_id = ar.alert_id
		`).
		Where("sar.alert_id = ?", alertId).
		Scan(&ar)
	if db.Error != nil {
		return ar, fmt.Errorf("%w, detail: %v", NoAlert, db.Error)
	}
	if db.RowsAffected < 1 {
		return ar, NoAlert
	}
	return ar, nil
}

func (g *Gorm) GetAlertRecipients(alertId string) ([]string, error) {
	return nil, fmt.Errorf("not implemented")
}
func (g *Gorm) UpdateAlertState(alert AlertState) error {
	return fmt.Errorf("not implemented")
}
func (g *Gorm) StoreAlertRule(dbRecord Schema) error {
	var err error
	if err = gorm.G[DeviceAlertRule](g.db).CreateInBatches(ctx,
		&dbRecord.DeviceAlerts, len(dbRecord.DeviceAlerts)); err != nil {
		return err
	}
	if err = gorm.G[AlertRecipient](g.db).CreateInBatches(ctx,
		&dbRecord.AlertRecipients, len(dbRecord.AlertRecipients)); err != nil {
		return err
	}
	if err = gorm.G[StoredAlertRule](g.db).CreateInBatches(ctx,
		&dbRecord.AlertRules, len(dbRecord.AlertRules)); err != nil {
		return err
	}
	return nil
}

func (g *Gorm) UpdateAlertRule(alertId string, dbRecord Schema) error {
	return fmt.Errorf("not implemented")
}

// NOTE: could return alertstore.AlertExists
func (g *Gorm) CheckAlertRuleExists(ar AlertRule) (string, error) {
	var alertId string
	err := g.db.Table("stored_alert_rules").
		Joins(
			`JOIN device_alert_rules ON
			stored_alert_rules.alert_id = device_alert_rules.alert_id`,
		).
		Where("device_alert_rules.device_id = ?", ar.DeviceId).
		Where("device_alert_rules.query_field = ?", ar.QueryField).
		Where("stored_alert_rules.duration = ?", ar.Duration).
		Where("stored_alert_rules.alert_range = ?", ar.AlertRange).
		Select("device_alert_rules.alert_id").
		Limit(1).
		Scan(&alertId).Error
	if alertId != "" {
		err = fmt.Errorf("%w. Detail: %v", AlertExists, err)
	}
	return alertId, err
}

func (g *Gorm) DeleteAlertState(alertId string) error {
	return fmt.Errorf("not implemented")
}

// NOTE: could return alertstore.NoAlert
func (g *Gorm) DeleteAlertRule(alertId string) (AlertRule, error) {
	return AlertRule{}, fmt.Errorf("not implemented")
}
