package store

import (
	"context"
	"database/sql"
	"fmt"
)

// ChangeProbe reports whether any other connection has committed to the
// database since it last looked: another process (mcp-serve, swamp fetch)
// or this process's own pool. The TUI polls it to know when to reload
// (RFC 0007, step 7).
//
// It reads PRAGMA data_version, which only moves for commits made through
// other connections, and is only meaningful compared with an earlier
// value from the same connection. So the probe holds one connection of
// its own, which it never writes through, for its whole life: a pooled
// query could land on a different connection each time.
type ChangeProbe struct {
	conn    *sql.Conn
	version int64
}

// NewChangeProbe takes a connection from s's pool for the probe and
// records the current version. Close it when done, or the pool loses
// that connection for good. It relies on the pool having room for more
// connections (store.Open sets no limit): capped at one, every other
// query would wait on the probe forever.
func (s *Store) NewChangeProbe(ctx context.Context) (*ChangeProbe, error) {
	conn, err := s.sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: change probe connection: %w", err)
	}
	p := &ChangeProbe{conn: conn}
	if p.version, err = p.dataVersion(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return p, nil
}

// Changed reports whether anything was committed since the probe was made
// or Changed last returned true.
func (p *ChangeProbe) Changed(ctx context.Context) (bool, error) {
	version, err := p.dataVersion(ctx)
	if err != nil {
		return false, err
	}
	if version == p.version {
		return false, nil
	}
	p.version = version
	return true, nil
}

// Close returns the probe's connection to the pool.
func (p *ChangeProbe) Close() error {
	return p.conn.Close()
}

func (p *ChangeProbe) dataVersion(ctx context.Context) (int64, error) {
	var version int64
	if err := p.conn.QueryRowContext(ctx, "PRAGMA data_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("store: read data_version: %w", err)
	}
	return version, nil
}
