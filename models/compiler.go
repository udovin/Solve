package models

import (
	"database/sql"
	"encoding/json"

	"github.com/udovin/gosql"
)

type CompilerConfig struct {
}

// Compiler represents compiler.
type Compiler struct {
	ID      int64  `db:"id"`
	OwnerID NInt64 `db:"owner_id"`
	Name    string `db:"name"`
	Config  JSON   `db:"config"`
}

// ObjectID returns ID of compiler.
func (o Compiler) ObjectID() int64 {
	return o.ID
}

// Clone create copy of compiler.
func (o Compiler) Clone() Compiler {
	o.Config = o.Config.Clone()
	return o
}

func (o Compiler) GetConfig() (CompilerConfig, error) {
	var config CompilerConfig
	err := json.Unmarshal(o.Config, &config)
	return config, err
}

func (o *Compiler) SetConfig(config CompilerConfig) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	o.Config = raw
	return nil
}

// CompilerEvent represents compiler event.
type CompilerEvent struct {
	baseEvent
	Compiler
}

// Object returns event compiler.
func (e CompilerEvent) Object() Compiler {
	return e.Compiler
}

// SetObject sets event compiler.
func (e *CompilerEvent) SetObject(o Compiler) {
	e.Compiler = o
}

// CompilerStore represents store for compilers.
type CompilerStore struct {
	baseStore[Compiler, CompilerEvent]
	compilers map[int64]Compiler
}

// Get returns compiler by specified ID.
func (s *CompilerStore) Get(id int64) (Compiler, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if compiler, ok := s.compilers[id]; ok {
		return compiler.Clone(), nil
	}
	return Compiler{}, sql.ErrNoRows
}

func (s *CompilerStore) All() ([]Compiler, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	var compilers []Compiler
	for _, compiler := range s.compilers {
		compilers = append(compilers, compiler)
	}
	return compilers, nil
}

func (s *CompilerStore) reset() {
	s.compilers = map[int64]Compiler{}
}

func (s *CompilerStore) makeObject(id int64) Compiler {
	return Compiler{ID: id}
}

func (s *CompilerStore) makeObjectEvent(typ EventType) CompilerEvent {
	return CompilerEvent{baseEvent: makeBaseEvent(typ)}
}

func (s *CompilerStore) onCreateObject(compiler Compiler) {
	s.compilers[compiler.ID] = compiler
}

func (s *CompilerStore) onDeleteObject(id int64) {
	if compiler, ok := s.compilers[id]; ok {
		delete(s.compilers, compiler.ID)
	}
}

// NewCompilerStore creates a new instance of CompilerStore.
func NewCompilerStore(db *gosql.DB, table, eventTable string) *CompilerStore {
	impl := &CompilerStore{}
	impl.baseStore = makeBaseStore[Compiler, CompilerEvent](
		db, table, eventTable, impl,
	)
	return impl
}
