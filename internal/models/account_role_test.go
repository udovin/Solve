package models

import (
	"database/sql"
	"testing"
)

type accountRoleStoreTest struct{}

func (t *accountRoleStoreTest) prepareDB(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`CREATE TABLE "account_role" (` +
			`"id" integer PRIMARY KEY,` +
			`"account_id" integer NOT NULL,` +
			`"role_id" integer NOT NULL)`,
	); err != nil {
		return err
	}
	_, err := tx.Exec(
		`CREATE TABLE "account_role_event" (` +
			`"event_id" integer PRIMARY KEY,` +
			`"event_kind" int8 NOT NULL,` +
			`"event_time" bigint NOT NULL,` +
			`"event_account_id" integer NULL,` +
			`"id" integer NOT NULL,` +
			`"account_id" integer NOT NULL,` +
			`"role_id" integer NOT NULL)`,
	)
	return err
}

func (t *accountRoleStoreTest) newStore() CachedStore {
	return NewAccountRoleStore(testDB, "account_role", "account_role_event")
}

func (t *accountRoleStoreTest) newObject() object {
	return AccountRole{}
}

func (t *accountRoleStoreTest) createObject(
	s CachedStore, tx *sql.Tx, o object,
) (object, error) {
	role := o.(AccountRole)
	if err := s.(*AccountRoleStore).Create(wrapContext(tx), &role); err != nil {
		return AccountRole{}, err
	}
	return role, nil
}

func (t *accountRoleStoreTest) updateObject(
	s CachedStore, tx *sql.Tx, o object,
) (object, error) {
	return o, s.(*AccountRoleStore).Update(wrapContext(tx), o.(AccountRole))
}

func (t *accountRoleStoreTest) deleteObject(
	s CachedStore, tx *sql.Tx, id int64,
) error {
	return s.(*AccountRoleStore).Delete(wrapContext(tx), id)
}

func TestUserRoleStore(t *testing.T) {
	testSetup(t)
	defer testTeardown(t)
	tester := CachedStoreTester{&accountRoleStoreTest{}}
	tester.Test(t)
}
