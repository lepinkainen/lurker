package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiskBlobsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d := &DiskBlobs{Dir: dir}
	ctx := context.Background()

	if ok, err := d.Exists(ctx, "abc.png"); err != nil || ok {
		t.Fatalf("Exists before Put = %v (err=%v), want false", ok, err)
	}
	if err := d.Put(ctx, "abc.png", []byte("bytes"), "image/png"); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists(ctx, "abc.png"); err != nil || !ok {
		t.Fatalf("Exists after Put = %v (err=%v), want true", ok, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "abc.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("bytes")) {
		t.Fatalf("stored %q, want %q", got, "bytes")
	}
	if err := d.Delete(ctx, "abc.png"); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists(ctx, "abc.png"); err != nil || ok {
		t.Fatalf("Exists after Delete = %v (err=%v), want false", ok, err)
	}
}

func TestDiskBlobsPutCreatesNestedKeyAndRefusesOverwrite(t *testing.T) {
	d := &DiskBlobs{Dir: t.TempDir()}
	ctx := context.Background()

	if err := d.Put(ctx, "nested/dir/abc.png", []byte("first"), "image/png"); err != nil {
		t.Fatal(err)
	}
	// A second Put on the same key must surface os.ErrExist so the caller
	// draws a fresh media id instead of clobbering an existing upload.
	err := d.Put(ctx, "nested/dir/abc.png", []byte("second"), "image/png")
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("second Put err = %v, want os.ErrExist", err)
	}
	got, readErr := os.ReadFile(filepath.Join(d.Dir, "nested", "dir", "abc.png"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, []byte("first")) {
		t.Fatalf("content = %q, want %q", got, "first")
	}
}

func TestDiskBlobsDeleteMissingIsNotAnError(t *testing.T) {
	d := &DiskBlobs{Dir: t.TempDir()}
	if err := d.Delete(context.Background(), "never-there.png"); err != nil {
		t.Fatalf("Delete missing key = %v, want nil", err)
	}
}
