package db

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var networkNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ErrInvalidNetworkName is returned when a network name does not match the
// implementation spec's allowed pattern.
var ErrInvalidNetworkName = errors.New("invalid network name")

// ValidateNetworkName enforces the v1 network naming rule.
func ValidateNetworkName(name string) error {
	if !networkNameRE.MatchString(name) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidNetworkName, name, networkNameRE.String())
	}
	return nil
}

// NormalizeNetworkName returns the case-insensitive uniqueness key for a
// validated network name.
func NormalizeNetworkName(name string) string {
	return strings.ToLower(name)
}

// LogDBFilename derives the per-network log DB filename for a validated name.
func LogDBFilename(name string) (string, error) {
	if err := ValidateNetworkName(name); err != nil {
		return "", err
	}
	return NormalizeNetworkName(name) + ".db", nil
}

// LogDBPath derives the per-network log DB path rooted under dataDir.
func LogDBPath(dataDir, name string) (string, error) {
	filename, err := LogDBFilename(name)
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, filename), nil
}
