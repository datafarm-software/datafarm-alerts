package alertstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ctx = context.Background()

type Mode int

const (
	Production = iota
	Testing
)

type Gorm struct {
	db         *gorm.DB
	mode       Mode
	dbFilename string
}

func NewGorm(mode Mode, dbName string) (AlertStore, error) {
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
	if err = g.db.AutoMigrate(&StoredLinearCalibration{}); err != nil {
		return err
	}
	stmts := []string{
		`DROP VIEW IF EXISTS alert_rule_view`,
		`CREATE VIEW alert_rule_view AS
		SELECT
			sar.alert_id,
			sar.name,
			dar.device_id,
			dar.query_field,
			sar.range_values,
			sar.duration,
			sar.alert_type,
			sar.alert_level,
		    lc.lc_raw_min,
			lc.lc_raw_max,
			lc.lc_cal_min,
			lc.lc_cal_max,
			json_group_array(ar.email) AS email_addresses
		FROM stored_alert_rules sar
		LEFT JOIN stored_linear_calibrations lc
		    ON lc.alert_id = sar.alert_id
		LEFT JOIN device_alert_rules dar
			ON dar.alert_id = sar.alert_id
		LEFT JOIN alert_recipients ar
			ON ar.alert_id = sar.alert_id
		GROUP BY
			sar.alert_id,
			sar.name,
			dar.device_id,
			dar.query_field,
			sar.range_values,
			sar.duration,
			sar.alert_type,
			sar.alert_level
		`,
	}
	for _, stmt := range stmts {
		if err = g.db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return g.Store(dbRecord)
}

func (g *Gorm) GetSnapshot() Schema {
	device_alert_rules, err := gorm.G[DeviceAlertRule](g.db).Find(ctx)
	if err != nil {
		return Schema{}
	}
	alertRecipients, err := gorm.G[AlertRecipient](g.db).Find(ctx)
	if err != nil {
		return Schema{}
	}
	stored_alert_rules, err := gorm.G[StoredAlertRule](g.db).Find(ctx)
	if err != nil {
		return Schema{}
	}
	alertStates, err := gorm.G[AlertState](g.db).Find(ctx)
	if err != nil {
		return Schema{}
	}
	lc, err := gorm.G[StoredLinearCalibration](g.db).Find(ctx)
	if err != nil {
		return Schema{}
	}
	return Schema{
		DeviceAlerts:       device_alert_rules,
		AlertRecipients:    alertRecipients,
		AlertStates:        alertStates,
		AlertRules:         stored_alert_rules,
		LinearCalibrations: lc,
	}
}

// NOTE: could return alertstore.NoAlert
func (g *Gorm) GetAlertState(alertId string) (AlertState, error) {
	var as AlertState
	var err error
	db := g.db.Table("alert_states").Where("id = ?", alertId).First(&as)
	err = db.Error
	if db.RowsAffected < 1 {
		err = NoAlert
	}
	return as, err
}

func (g *Gorm) GetAlertRule(alertId string) (AlertRule, error) {
	var ar AlertRule
	ar.AlertId = alertId
	db := g.db.
		Model(&AlertRule{}).
		Table("alert_rule_view").
		Where("alert_id = ?", alertId).
		Take(&ar)
	if db.Error != nil {
		return ar, fmt.Errorf("%w, detail: %v", NoAlert, db.Error)
	}
	if db.RowsAffected < 1 {
		return ar, NoAlert
	}
	return ar, nil
}

func (g *Gorm) GetAlertRecipients(alertId string) ([]string, error) {
	var emails []string
	var err error
	db := g.db.Model(&AlertRecipient{}).
		Where("alert_id = ?", alertId).
		Pluck("email", &emails)
	err = db.Error
	if db.RowsAffected < 1 || len(emails) < 1 {
		err = fmt.Errorf("no recipients")
	}
	return emails, err
}

func (g *Gorm) UpdateAlertState(alert AlertState) error {
	return g.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		UpdateAll: true,
	}).Create(&alert).
		Error
}

func (g *Gorm) Store(dbRecord Schema) error {
	var err error
	return g.db.Transaction(func(tx *gorm.DB) error {
		if len(dbRecord.DeviceAlerts) > 0 {
			if err = gorm.G[DeviceAlertRule](g.db).CreateInBatches(ctx,
				&dbRecord.DeviceAlerts, len(dbRecord.DeviceAlerts)); err != nil {
				return err
			}
		}
		if len(dbRecord.AlertRecipients) > 0 {
			if err = gorm.G[AlertRecipient](g.db).CreateInBatches(ctx,
				&dbRecord.AlertRecipients, len(dbRecord.AlertRecipients)); err != nil {
				return err
			}
		}
		if len(dbRecord.AlertRules) > 0 {
			if err = gorm.G[StoredAlertRule](g.db).CreateInBatches(ctx,
				&dbRecord.AlertRules, len(dbRecord.AlertRules)); err != nil {
				return err
			}
		}
		if len(dbRecord.LinearCalibrations) > 0 {
			if err = gorm.G[StoredLinearCalibration](g.db).CreateInBatches(ctx,
				&dbRecord.LinearCalibrations, len(dbRecord.LinearCalibrations)); err != nil {
				return err
			}
		}
		if len(dbRecord.AlertStates) > 0 {
			err = gorm.G[AlertState](g.db, clause.OnConflict{
				Columns:   []clause.Column{{Name: "id"}},
				UpdateAll: true,
			}).CreateInBatches(ctx, &dbRecord.AlertStates, len(dbRecord.AlertStates))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (g *Gorm) UpdateAlertRule(alertId string, dbRecord Schema) error {
	return g.db.Transaction(func(tx *gorm.DB) error {
		var err error
		for _, da := range dbRecord.DeviceAlerts {
			if err = tx.Table("device_alert_rules").
				Where("alert_id", da.AlertId).
				Update("device_id", da.DeviceId).
				Update("query_field", da.QueryField).Error; err != nil {
				break
			}
		}
		if err != nil {
			return err
		}
		for _, ar := range dbRecord.AlertRecipients {
			if err = tx.Clauses(
				clause.OnConflict{
					Columns:   []clause.Column{{Name: "alert_id"}, {Name: "email"}},
					DoNothing: true,
				}).
				Table("alert_recipients").
				Create(&ar).Error; err != nil {
				break
			}
		}
		if err != nil {
			return err
		}
		for _, sar := range dbRecord.AlertRules {
			if err = tx.Table("stored_alert_rules").
				Where("alert_id = ?", sar.AlertId).
				Save(&sar).Error; err != nil {
				break
			}
		}
		if err != nil {
			return err
		}
		for _, lc := range dbRecord.LinearCalibrations {
			if err = tx.Table("stored_linear_calibrations").
				Where("alert_id", lc.AlertId).
				Save(&lc).Error; err != nil {
				break
			}
		}
		if err != nil {
			return err
		}
		return nil
	})
}

// NOTE: could return alertstore.AlertExists
func (g *Gorm) CheckAlertRuleExists(ar AlertRule) (string, error) {
	var alertId string
	var err error
	db := g.db.Table("stored_alert_rules").
		Joins(
			`JOIN device_alert_rules ON
			stored_alert_rules.alert_id = device_alert_rules.alert_id`,
		).
		Where("device_alert_rules.device_id = ?", ar.DeviceId).
		Where("device_alert_rules.query_field = ?", ar.QueryField).
		Where("stored_alert_rules.duration = ?", ar.Duration).
		Where("stored_alert_rules.alert_type = ?", ar.AlertType)
	if ar.RangeValues != nil {
		jsonRangeValues, err := json.Marshal(ar.RangeValues)
		if err != nil {
			return alertId, fmt.Errorf("marshalling range values: %v", err)
		}
		db = db.Where("stored_alert_rules.range_values = ?", string(jsonRangeValues))
	}
	err = db.Select("device_alert_rules.alert_id").
		Limit(1).
		Scan(&alertId).Error
	if alertId != "" {
		err = fmt.Errorf("%w. Detail: %v", AlertExists, err)
	}
	return alertId, err
}

func (g *Gorm) DeleteAlertState(alertId string) error {
	if err := g.db.Table("alert_states").
		Where("id = ?", alertId).
		Delete("*").Error; err != nil {
		return err
	}
	return nil
}

// NOTE: could return alertstore.NoAlert
func (g *Gorm) DeleteAlertRule(alertId string) (AlertRule, error) {
	ar, err := g.GetAlertRule(alertId)
	if err != nil {
		return ar, err
	}
	if err := g.db.Transaction(func(tx *gorm.DB) error {
		if err = tx.Table("device_alert_rules").
			Where("alert_id = ?", alertId).
			Delete("*").Error; err != nil {
			return err
		}
		if err = tx.Table("alert_recipients").
			Where("alert_id = ?", alertId).
			Delete("*").Error; err != nil {
			return err
		}
		if err = tx.Table("stored_alert_rules").
			Where("alert_id = ?", alertId).
			Delete("*").Error; err != nil {
			return err
		}
		if err = tx.Table("stored_linear_calibrations").
			Where("alert_id = ?", alertId).
			Delete("*").Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return ar, err
	}
	return ar, nil
}

func (g *Gorm) RemoveUsersFromAlertRule(ars []AlertRecipient) error {
	return g.db.Transaction(func(tx *gorm.DB) error {
		for _, ar := range ars {
			if err := tx.Table("alert_recipients").
				Where("alert_id = ?", ar.AlertId).
				Where("email = ?", ar.Email).
				Delete("*").Error; err != nil {
				return err
			}
		}
		return nil
	})
}
