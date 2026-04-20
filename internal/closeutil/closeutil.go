package closeutil

import (
	"io"
	"log/slog"
)

// Ignore closes c and logs any error.
func Ignore(c io.Closer, attrs ...any) {
	if err := c.Close(); err != nil {
		slog.Warn("close failed", append(attrs, "err", err)...)
	}
}
