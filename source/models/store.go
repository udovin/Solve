package models

import (
	"database/sql"
	"fmt"
	"sync"
	"time"
)

type ChangeType int8

const (
	CreateChange ChangeType = 1
	DeleteChange ChangeType = 2
	UpdateChange ChangeType = 3
)

type RowScan interface {
	Scan(dest ...interface{}) error
}

type Change interface {
	ChangeID() int64
	ChangeType() ChangeType
	ChangeTime() int64
	ChangeData() interface{}
}

type ChangeStore interface {
	GetDB() *sql.DB
	ChangeTableName() string
	scanChange(scan RowScan) (Change, error)
	createChangeTx(
		tx *sql.Tx, changeType ChangeType,
		changeTime int64, data interface{},
	) (Change, error)
	applyChange(change Change)
}

type ChangeManager struct {
	store        ChangeStore
	lastChangeID int64
	mutex        sync.Mutex
}

func NewChangeManager(store ChangeStore) *ChangeManager {
	return &ChangeManager{store: store}
}

func (m *ChangeManager) LockTx(tx *sql.Tx) error {
	_, err := tx.Exec(
		fmt.Sprintf(`LOCK TABLE "%s"`, m.store.ChangeTableName()),
	)
	return err
}

func (m *ChangeManager) Sync() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	rows, err := m.store.GetDB().Query(
		fmt.Sprintf(
			`SELECT * FROM "%s" WHERE "change_id" > $1 ORDER BY "change_id"`,
			m.store.ChangeTableName(),
		),
		m.lastChangeID,
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			panic(err)
		}
	}()
	for rows.Next() {
		change, err := m.store.scanChange(rows)
		if err != nil {
			return err
		}
		m.applyChange(change)
	}
	return nil
}

func (m *ChangeManager) SyncTx(tx *sql.Tx) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	rows, err := tx.Query(
		fmt.Sprintf(
			`SELECT * FROM "%s" WHERE "change_id" > $1 ORDER BY "change_id"`,
			m.store.ChangeTableName(),
		),
		m.lastChangeID,
	)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			panic(err)
		}
	}()
	for rows.Next() {
		change, err := m.store.scanChange(rows)
		if err != nil {
			return err
		}
		m.applyChange(change)
	}
	return nil
}

func (m *ChangeManager) Change(
	changeType ChangeType, data interface{},
) (Change, error) {
	tx, err := m.store.GetDB().Begin()
	if err != nil {
		return nil, err
	}
	change, err := m.ChangeTx(tx, changeType, data)
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return change, nil
}

func (m *ChangeManager) ChangeTx(
	tx *sql.Tx, changeType ChangeType, data interface{},
) (Change, error) {
	if err := m.LockTx(tx); err != nil {
		return nil, err
	}
	if err := m.SyncTx(tx); err != nil {
		return nil, err
	}
	change, err := m.store.createChangeTx(
		tx, changeType, time.Now().Unix(), data,
	)
	if err != nil {
		return nil, err
	}
	m.applyChange(change)
	return change, nil
}

func (m *ChangeManager) applyChange(change Change) {
	m.store.applyChange(change)
	m.lastChangeID = change.ChangeID()
}
