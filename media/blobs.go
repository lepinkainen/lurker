package media

import (
	"context"
	"fmt"
	"os"
	"path"
)

// Blobs stores the bytes of media variants. There is no default and no
// fallback: the backend is chosen explicitly in config and injected by main,
// so a misconfigured deployment can never quietly start filling a server's
// disk. A nil Blobs on Service means uploads are disabled, not "use the
// disk".
type Blobs interface {
	// Put writes data under key. Implementations must not overwrite an
	// existing key: they return an error wrapping os.ErrExist so the caller
	// can draw a fresh media id.
	Put(ctx context.Context, key string, data []byte, contentType string) error
	// Exists reports whether key is already stored.
	Exists(ctx context.Context, key string) (bool, error)
	// Delete removes key. A missing key is not an error: cleanup callers may
	// race a Put that never landed.
	Delete(ctx context.Context, key string) error
}

// DiskBlobs stores variants on the local filesystem under Dir. It is a peer
// of the object-storage backend, not a default — main selects it only when
// config says `media.backend: disk`.
type DiskBlobs struct {
	Dir string
}

// withRoot runs fn against Dir opened as an os.Root, which resolves every
// path inside it and refuses to escape — so a key can never traverse out of
// the upload directory, however it was built.
func (d *DiskBlobs) withRoot(fn func(*os.Root) error) error {
	root, err := os.OpenRoot(d.Dir)
	if err != nil {
		return fmt.Errorf("open upload dir: %w", err)
	}
	defer func() { _ = root.Close() }()
	return fn(root)
}

// Put writes data to Dir/key, creating parent directories. O_EXCL makes a
// pre-existing key surface as os.ErrExist rather than clobbering it.
// contentType is unused: the extension in key carries it for local serving.
func (d *DiskBlobs) Put(_ context.Context, key string, data []byte, _ string) error {
	return d.withRoot(func(root *os.Root) error {
		if dir := path.Dir(key); dir != "." && dir != "/" {
			if err := root.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create upload dir: %w", err)
			}
		}
		f, err := root.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("create upload file: %w", err)
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil {
			_ = root.Remove(key)
			return fmt.Errorf("write upload file: %w", writeErr)
		}
		if closeErr != nil {
			_ = root.Remove(key)
			return fmt.Errorf("close upload file: %w", closeErr)
		}
		return nil
	})
}

// Exists reports whether Dir/key is present.
func (d *DiskBlobs) Exists(_ context.Context, key string) (bool, error) {
	var found bool
	err := d.withRoot(func(root *os.Root) error {
		_, statErr := root.Stat(key)
		switch {
		case statErr == nil:
			found = true
			return nil
		case os.IsNotExist(statErr):
			return nil
		default:
			return statErr
		}
	})
	return found, err
}

// Delete removes Dir/key, tolerating a missing file.
func (d *DiskBlobs) Delete(_ context.Context, key string) error {
	return d.withRoot(func(root *os.Root) error {
		if err := root.Remove(key); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}
