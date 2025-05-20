package store

import (
	"io"

	"github.com/gofrs/flock"
	"github.com/hashicorp/raft"
)

type fsm struct {
	dataFile string
	lock     *flock.Flock
}

func (f *fsm) Apply(l *raft.Log) any {
	return nil
}

func (f *fsm) Snapshot() (raft.FSMSnapshot, error) {
	return nil, nil
}

func (f *fsm) Restore(old io.ReadCloser) error {
	return nil
}
