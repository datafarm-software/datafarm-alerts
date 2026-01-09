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
