package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/beyzbackup/beyz-backup/internal/agent/logging"
)

// Appender is the pluggable, append-only store for audit records. The in-memory
// and JSONL implementations live here; the bbolt-backed store lands in S1-T10.
type Appender interface {
	// Append stores a fully-formed record (seq + hashes already set).
	Append(r Record) error
	// Head returns the last record's seq and this_hash. ok is false when empty.
	Head() (seq uint64, thisHash string, ok bool, err error)
	// Records returns all records in seq order (for verification/export).
	Records() ([]Record, error)
}

// MemoryAppender is an in-memory Appender (tests, and pre-T10 wiring).
type MemoryAppender struct {
	mu      sync.Mutex
	records []Record
}

// NewMemoryAppender returns an empty in-memory appender.
func NewMemoryAppender() *MemoryAppender { return &MemoryAppender{} }

// Append stores a record.
func (a *MemoryAppender) Append(r Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, r)
	return nil
}

// Head returns the last record's seq and this_hash.
func (a *MemoryAppender) Head() (uint64, string, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.records) == 0 {
		return 0, "", false, nil
	}
	last := a.records[len(a.records)-1]
	return last.Seq, last.ThisHash, true, nil
}

// Records returns a copy of all records.
func (a *MemoryAppender) Records() ([]Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Record, len(a.records))
	copy(out, a.records)
	return out, nil
}

// FileAppender persists records as JSON Lines (one record per line). It is a
// simple durable Appender for Sprint 1; the transactional bbolt store is S1-T10.
type FileAppender struct {
	mu   sync.Mutex
	path string
}

// NewFileAppender returns a JSONL file appender writing to path.
func NewFileAppender(path string) *FileAppender { return &FileAppender{path: path} }

// Append writes the record as one JSON line.
func (a *FileAppender) Append(r Record) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- caller-provided audit path
	if err != nil {
		return fmt.Errorf("audit: opening %q: %w", a.path, err)
	}
	defer func() { _ = f.Close() }()
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("audit: marshaling record: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("audit: writing record: %w", err)
	}
	return nil
}

// Records reads and parses all records.
func (a *FileAppender) Records() ([]Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.Open(a.path) // #nosec G304 -- caller-provided audit path
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("audit: opening %q: %w", a.path, err)
	}
	defer func() { _ = f.Close() }()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("audit: parsing record: %w", err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("audit: reading %q: %w", a.path, err)
	}
	return out, nil
}

// Head returns the last record's seq and this_hash.
func (a *FileAppender) Head() (uint64, string, bool, error) {
	recs, err := a.Records()
	if err != nil {
		return 0, "", false, err
	}
	if len(recs) == 0 {
		return 0, "", false, nil
	}
	last := recs[len(recs)-1]
	return last.Seq, last.ThisHash, true, nil
}

// Identity is the immutable per-emitter context applied to every record.
type Identity struct {
	TenantID     string
	ParentOrgID  string // "" -> null
	DeviceID     string
	DeviceGUID   string // anchors the device-bound genesis
	AgentVersion string
	Source       string // must be SourceAgent in Sprint 1
}

// Event is the per-call input. The emitter fills the chain/identity/timestamp
// fields and computes the hashes.
type Event struct {
	EventType string
	Category  string
	Severity  string
	Outcome   string
	Actor     string
	Target    *Target
	TraceID   string
	Detail    map[string]any
}

// Emitter builds, hashes, chains, stores, and streams audit records. It is safe
// for concurrent use.
type Emitter struct {
	mu          sync.Mutex
	appender    Appender
	stream      io.Writer
	id          Identity
	now         func() time.Time
	headSeq     uint64
	headHash    string
	genesisHash string
}

// Option configures an Emitter.
type Option func(*Emitter)

// WithClock overrides the time source (for tests).
func WithClock(now func() time.Time) Option { return func(e *Emitter) { e.now = now } }

// New creates an Emitter that appends to store and mirrors records to stream
// (stream may be nil). It resumes the chain from the store's head.
func New(store Appender, stream io.Writer, id Identity, opts ...Option) (*Emitter, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: nil appender", ErrInvalidField)
	}
	if id.Source == "" {
		id.Source = SourceAgent
	}
	if _, ok := validSources[id.Source]; !ok {
		return nil, fmt.Errorf("%w: source %q", ErrInvalidField, id.Source)
	}
	e := &Emitter{
		appender:    store,
		stream:      stream,
		id:          id,
		now:         func() time.Time { return time.Now().UTC() },
		genesisHash: GenesisPrevHash(id.DeviceGUID),
	}
	for _, o := range opts {
		o(e)
	}
	seq, hash, ok, err := store.Head()
	if err != nil {
		return nil, fmt.Errorf("audit: reading chain head: %w", err)
	}
	if ok {
		e.headSeq, e.headHash = seq, hash
	} else {
		e.headSeq, e.headHash = 0, e.genesisHash
	}
	return e, nil
}

// Emit validates, builds, hashes, chains, stores, and streams an event, and
// returns the finalized record.
func (e *Emitter) Emit(ev Event) (Record, error) {
	if err := validateEvent(ev); err != nil {
		return Record{}, err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	r := Record{
		SchemaVersion: SchemaVersion,
		Seq:           e.headSeq + 1,
		PrevHash:      e.headHash,
		TSLocal:       e.now().Format(time.RFC3339),
		Source:        e.id.Source,
		EventType:     ev.EventType,
		Category:      ev.Category,
		Severity:      ev.Severity,
		Outcome:       ev.Outcome,
		Actor:         ev.Actor,
		TenantID:      e.id.TenantID,
		DeviceID:      e.id.DeviceID,
		AgentVersion:  e.id.AgentVersion,
		Target:        ev.Target,
		TraceID:       ev.TraceID,
		Detail:        redactDetail(ev.Detail),
	}
	if e.id.ParentOrgID != "" {
		r.ParentOrgID = &e.id.ParentOrgID
	}

	thisHash, err := computeThisHash(r)
	if err != nil {
		return Record{}, err
	}
	r.ThisHash = thisHash

	if err := e.appender.Append(r); err != nil {
		return Record{}, fmt.Errorf("audit: appending record: %w", err)
	}
	if err := e.writeStream(r); err != nil {
		return Record{}, err
	}

	e.headSeq, e.headHash = r.Seq, r.ThisHash
	return r, nil
}

// Head returns the current chain head (seq, this_hash).
func (e *Emitter) Head() (uint64, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.headSeq, e.headHash
}

func (e *Emitter) writeStream(r Record) error {
	if e.stream == nil {
		return nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("audit: marshaling record for stream: %w", err)
	}
	// Defense-in-depth: detail is already redacted at Emit time.
	if _, err := io.WriteString(e.stream, logging.Redact(string(b))+"\n"); err != nil {
		return fmt.Errorf("audit: writing audit stream: %w", err)
	}
	return nil
}

func validateEvent(ev Event) error {
	if !IsValidEventType(ev.EventType) {
		return fmt.Errorf("%w: %q", ErrUnknownEventType, ev.EventType)
	}
	if _, ok := validCategories[ev.Category]; !ok {
		return fmt.Errorf("%w: category %q", ErrInvalidField, ev.Category)
	}
	if _, ok := validSeverities[ev.Severity]; !ok {
		return fmt.Errorf("%w: severity %q", ErrInvalidField, ev.Severity)
	}
	if _, ok := validOutcomes[ev.Outcome]; !ok {
		return fmt.Errorf("%w: outcome %q", ErrInvalidField, ev.Outcome)
	}
	return nil
}

// redactDetail scrubs known secret shapes from string values (detail is
// redaction-first; this is defense in depth).
func redactDetail(d map[string]any) map[string]any {
	if d == nil {
		return nil
	}
	out := make(map[string]any, len(d))
	for k, v := range d {
		if s, ok := v.(string); ok {
			out[k] = logging.Redact(s)
		} else {
			out[k] = v
		}
	}
	return out
}
