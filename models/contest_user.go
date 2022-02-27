package models

import (
	"database/sql"

	"github.com/udovin/gosql"
)

// ContestUser contains common information about contest user.
type ContestUser struct {
	ID           int64  `db:"id"`
	AccountID    int64  `db:"account_id"`
	ContestID    int64  `db:"contest_id"`
	Login        string `db:"login"`
	PasswordHash string `db:"password_hash"`
	PasswordSalt string `db:"password_salt"`
	Name         string `db:"name"`
}

// ObjectID returns ID of user.
func (o ContestUser) ObjectID() int64 {
	return o.ID
}

// Clone creates copy of user.
func (o ContestUser) Clone() ContestUser {
	return o
}

// ContestUserEvent represents an contest user event.
type ContestUserEvent struct {
	baseEvent
	ContestUser
}

// Object returns contest user.
func (e ContestUserEvent) Object() ContestUser {
	return e.ContestUser
}

// WithObject return copy of event with replaced contest user.
func (e ContestUserEvent) WithObject(o ContestUser) ObjectEvent[ContestUser] {
	e.ContestUser = o
	return e
}

// UserStore represents users store.
type ContestUserStore struct {
	baseStore[ContestUser, ContestUserEvent]
	users map[int64]ContestUser
}

// Get returns user by ID.
func (s *ContestUserStore) Get(id int64) (ContestUser, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if user, ok := s.users[id]; ok {
		return user.Clone(), nil
	}
	return ContestUser{}, sql.ErrNoRows
}

// DeleteTx deletes user with specified ID.
func (s *ContestUserStore) DeleteTx(tx gosql.WeakTx, id int64) error {
	_, err := s.createObjectEvent(tx, ContestUserEvent{
		makeBaseEvent(DeleteEvent),
		ContestUser{ID: id},
	})
	return err
}

func (s *ContestUserStore) reset() {
	s.users = map[int64]ContestUser{}
}

func (s *ContestUserStore) makeObjectEvent(typ EventType) ObjectEvent[ContestUser] {
	return ContestUserEvent{baseEvent: makeBaseEvent(typ)}
}

func (s *ContestUserStore) onCreateObject(user ContestUser) {
	s.users[user.ID] = user
}

func (s *ContestUserStore) onDeleteObject(user ContestUser) {
	delete(s.users, user.ID)
}

func (s *ContestUserStore) onUpdateObject(user ContestUser) {
	if old, ok := s.users[user.ID]; ok {
		s.onDeleteObject(old)
	}
	s.onCreateObject(user)
}

// NewContestUserStore creates new instance of contest user store.
func NewContestUserStore(
	db *gosql.DB, table, eventTable, salt string,
) *ContestUserStore {
	impl := &ContestUserStore{}
	impl.baseStore = makeBaseStore[ContestUser, ContestUserEvent](
		db, table, eventTable, impl,
	)
	return impl
}
