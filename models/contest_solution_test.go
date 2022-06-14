package models

import (
	"database/sql"
	"testing"

	"github.com/udovin/solve/db"
)

type contestSolutionStoreTest struct{}

func (t *contestSolutionStoreTest) prepareDB(tx *sql.Tx) error {
	if _, err := tx.Exec(
		`CREATE TABLE "contest_solution" (` +
			`"id" integer PRIMARY KEY,` +
			`"solution_id" integer NOT NULL,` +
			`"contest_id" integer NOT NULL,` +
			`"participant_id" integer NOT NULL,` +
			`"problem_id" integer NOT NULL)`,
	); err != nil {
		return err
	}
	_, err := tx.Exec(
		`CREATE TABLE "contest_solution_event" (` +
			`"event_id" integer PRIMARY KEY,` +
			`"event_type" int8 NOT NULL,` +
			`"event_time" bigint NOT NULL,` +
			`"event_account_id" integer NULL,` +
			`"id" integer NOT NULL,` +
			`"solution_id" integer NOT NULL,` +
			`"contest_id" integer NOT NULL,` +
			`"participant_id" integer NOT NULL,` +
			`"problem_id" integer NOT NULL)`,
	)
	return err
}

func (t *contestSolutionStoreTest) newStore() Store {
	return NewContestSolutionStore(
		testDB, "contest_solution", "contest_solution_event",
	)
}

func (t *contestSolutionStoreTest) newObject() db.Object {
	return ContestSolution{}
}

func (t *contestSolutionStoreTest) createObject(
	s Store, tx *sql.Tx, o db.Object,
) (db.Object, error) {
	solution := o.(ContestSolution)
	err := s.(*ContestSolutionStore).Create(wrapContext(tx), &solution)
	return solution, err
}

func (t *contestSolutionStoreTest) updateObject(
	s Store, tx *sql.Tx, o db.Object,
) (db.Object, error) {
	return o, s.(*ContestSolutionStore).Update(wrapContext(tx), o.(ContestSolution))
}

func (t *contestSolutionStoreTest) deleteObject(
	s Store, tx *sql.Tx, id int64,
) error {
	return s.(*ContestSolutionStore).Delete(wrapContext(tx), id)
}

func TestContestSolutionStore(t *testing.T) {
	testSetup(t)
	defer testTeardown(t)
	tester := StoreTester{&contestSolutionStoreTest{}}
	tester.Test(t)
}
