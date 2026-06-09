package audit

import "fmt"

// Verify checks the local hash chain of records (in seq order) for the device
// identified by deviceGUID. It detects sequence gaps, reordering or deletion,
// insertion, content tampering, chain grafting, and unknown event types.
//
// NOTE (§0.4): this proves integrity against accidental corruption and
// NON-privileged tampering. A SYSTEM/admin attacker can recompute the whole
// chain; detecting that requires off-device WORM anchoring (Sprint 8).
func Verify(records []Record, deviceGUID string) error {
	prev := GenesisPrevHash(deviceGUID)
	var expectSeq uint64 = 1

	for i := range records {
		r := records[i]
		if r.Seq != expectSeq {
			return fmt.Errorf("%w: at index %d got seq %d, want %d", ErrChainGap, i, r.Seq, expectSeq)
		}
		if r.PrevHash != prev {
			return fmt.Errorf("%w: at seq %d", ErrChainBroken, r.Seq)
		}
		if !IsValidEventType(r.EventType) {
			return fmt.Errorf("%w: at seq %d: %q", ErrUnknownEventType, r.Seq, r.EventType)
		}
		computed, err := computeThisHash(r)
		if err != nil {
			return err
		}
		if computed != r.ThisHash {
			return fmt.Errorf("%w: at seq %d", ErrTampered, r.Seq)
		}
		prev = r.ThisHash
		expectSeq++
	}
	return nil
}
