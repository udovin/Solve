package models

import (
	"database/sql"
	"testing"
)

type roleStoreTest struct{}

func (t *roleStoreTest) prepareDB(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`CREATE TABLE "role" (` +
			`"id" integer PRIMARY KEY,` +
			`"name" varchar(255) NOT NULL)`,
	); err != nil {
		return err
	}
	_, err := tx.Exec(
		`CREATE TABLE "role_event" (` +
			`"event_id" integer PRIMARY KEY,` +
			`"event_kind" int8 NOT NULL,` +
			`"event_time" bigint NOT NULL,` +
			`"event_account_id" integer NULL,` +
			`"id" integer NOT NULL,` +
			`"name" varchar(255) NOT NULL)`,
	)
	return err
}

func (t *roleStoreTest) newStore() CachedStore {
	return NewRoleStore(testDB, "role", "role_event")
}

func (t *roleStoreTest) newObject() object {
	return Role{}
}

func (t *roleStoreTest) createObject(
	s CachedStore, tx *sql.Tx, o object,
) (object, error) {
	object := o.(Role)
	err := s.(*RoleStore).Create(wrapContext(tx), &object)
	return object, err
}

func (t *roleStoreTest) updateObject(
	s CachedStore, tx *sql.Tx, o object,
) (object, error) {
	return o, s.(*RoleStore).Update(wrapContext(tx), o.(Role))
}

func (t *roleStoreTest) deleteObject(
	s CachedStore, tx *sql.Tx, id int64,
) error {
	return s.(*RoleStore).Delete(wrapContext(tx), id)
}

func TestRoleStore(t *testing.T) {
	testSetup(t)
	defer testTeardown(t)
	tester := CachedStoreTester{&roleStoreTest{}}
	tester.Test(t)
}
