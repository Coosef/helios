package state

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"

	"github.com/beyzbackup/beyz-backup/internal/agent/audit"
)

// AuditAppender returns a bbolt-backed audit.Appender over the audit-spool
// bucket. It satisfies the Appender interface consumed by audit.Emitter.
func (s *Store) AuditAppender() audit.Appender {
	return &bboltAppender{store: s}
}

type bboltAppender struct {
	store *Store
}

var _ audit.Appender = (*bboltAppender)(nil)

// Append stores a finalized record keyed by big-endian seq (so byte order == seq
// order), in a single atomic transaction.
func (a *bboltAppender) Append(r audit.Record) error {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	if a.store.closed {
		return fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("state: encoding audit record: %w", err)
	}
	return a.store.db.Update(func(tx *bolt.Tx) error {
		bkt := tx.Bucket([]byte(bucketAuditSpool))
		// Append-only: never silently overwrite an existing seq (that would mask
		// chain corruption). Fail closed on a duplicate.
		if bkt.Get(itob64(r.Seq)) != nil {
			return fmt.Errorf("%w: audit seq %d already present", ErrInvalidState, r.Seq)
		}
		return bkt.Put(itob64(r.Seq), b)
	})
}

// Head returns the last record's seq and this_hash (ok=false when empty).
func (a *bboltAppender) Head() (uint64, string, bool, error) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	if a.store.closed {
		return 0, "", false, fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	var (
		seq      uint64
		thisHash string
		ok       bool
	)
	err := a.store.db.View(func(tx *bolt.Tx) error {
		k, v := tx.Bucket([]byte(bucketAuditSpool)).Cursor().Last()
		if k == nil {
			return nil
		}
		var r audit.Record
		if err := json.Unmarshal(v, &r); err != nil {
			return fmt.Errorf("state: decoding audit head: %w", err)
		}
		seq, thisHash, ok = r.Seq, r.ThisHash, true
		return nil
	})
	if err != nil {
		return 0, "", false, err
	}
	return seq, thisHash, ok, nil
}

// Records returns all records in seq order.
func (a *bboltAppender) Records() ([]audit.Record, error) {
	a.store.mu.RLock()
	defer a.store.mu.RUnlock()
	if a.store.closed {
		return nil, fmt.Errorf("%w: store is closed", ErrInvalidState)
	}
	var out []audit.Record
	err := a.store.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketAuditSpool)).ForEach(func(_, v []byte) error {
			var r audit.Record
			if err := json.Unmarshal(v, &r); err != nil {
				return fmt.Errorf("state: decoding audit record: %w", err)
			}
			out = append(out, r)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
