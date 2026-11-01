package snowflake

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"syscall"

	"atlas.local/gateway/ports"
)

// classify marks an engine's failure the caller may retry
// (ports.ErrSourceUnavailable, which ports.ErrWarehouseUnavailable is):
// a connection that broke or was refused, a pool that is closed, or what
// the deployment's engine hook says is passing (R052). A failure because
// the call's context ended is that context's error, with the engine's own
// error kept in its text. Anything else is the engine's error, unchanged:
// a permanent failure the pipeline answers as internal.
func classify(ctx context.Context, err error, unavailable func(error) bool) error {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		if errors.Is(err, ctx.Err()) {
			return err
		}
		return fmt.Errorf("%w (the engine: %v)", ctx.Err(), err)
	case errors.Is(err, ports.ErrSourceUnavailable):
		return err
	case transient(err) || unavailable != nil && unavailable(err):
		return fmt.Errorf("%w: %w", ports.ErrSourceUnavailable, err)
	}
	return err
}

// transient reports a failure of the connection itself, whatever the
// driver.
func transient(err error) bool {
	var ne net.Error
	return errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) || errors.As(err, &ne) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE)
}
