package diskguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func plenty(string) (int64, error) { return 1 << 40, nil }

func TestVolumeLeaseSerializesSiblingPathsAndCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, err := Acquire(ctx, filepath.Join(root, "a"), Options{Query: plenty})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var queries atomic.Int32
	waited := make(chan struct{})
	finished := make(chan error, 1)
	cctx, ccancel := context.WithCancel(ctx)
	go func() {
		l, err := Acquire(cctx, filepath.Join(root, "b"), Options{Query: func(string) (int64, error) { queries.Add(1); return 1 << 40, nil }, OnWait: func() { close(waited) }})
		if l != nil {
			l.Close()
		}
		finished <- err
	}()
	select {
	case <-waited:
	case <-ctx.Done():
		t.Fatal("same-volume writer did not queue")
	}
	if queries.Load() != 0 {
		t.Fatal("queued operation queried obsolete capacity")
	}
	ccancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel queued lease: %v", err)
	}
	a.Close()
	b, err := Acquire(ctx, filepath.Join(root, "b"), Options{Query: plenty})
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	b.Close()
	volumes.Lock()
	n := len(volumes.gates)
	volumes.Unlock()
	if n != 0 {
		t.Fatalf("leases leaked %d volume records", n)
	}
}

func TestUnknownCapacityNeedsExplicitConfirmation(t *testing.T) {
	root := t.TempDir()
	unknown := func(string) (int64, error) { return 0, errors.New("query unavailable") }
	if _, err := Acquire(context.Background(), root, Options{Query: unknown}); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown capacity must stop: %v", err)
	}
	l, err := Acquire(context.Background(), root, Options{Query: unknown, AllowUnknown: true})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if _, err := l.Writer(io.Discard).Write([]byte("explicitly confirmed")); err != nil {
		t.Fatal(err)
	}
}

func TestLargeWriteIsCheckedDuringOneCall(t *testing.T) {
	queries := 0
	l, err := Acquire(context.Background(), t.TempDir(), Options{CheckEvery: 1024, Reserve: 2048, Query: func(string) (int64, error) {
		queries++
		if queries > 3 {
			return 2047, nil
		}
		return 1 << 20, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var out bytes.Buffer
	n, err := l.Writer(&out).Write(make([]byte, 8<<10))
	var ns *NoSpace
	if !errors.As(err, &ns) || n <= 0 || n >= 8<<10 || out.Len() != n {
		t.Fatalf("mid-write stop: n=%d size=%d err=%v", n, out.Len(), err)
	}
}

func TestReaderLimitsImplicitTempWrites(t *testing.T) {
	queries := 0
	l, err := Acquire(context.Background(), t.TempDir(), Options{CheckEvery: 1024, Reserve: 2048, Query: func(string) (int64, error) {
		queries++
		if queries > 3 {
			return 2047, nil
		}
		return 1 << 20, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	n, err := io.Copy(io.Discard, l.Reader(bytes.NewReader(make([]byte, 8<<10))))
	var ns *NoSpace
	if !errors.As(err, &ns) || n <= 0 || n >= 8<<10 {
		t.Fatalf("implicit writes must stop at boundary: n=%d err=%v", n, err)
	}
}

func TestDifferentRealVolumesDoNotBlock(t *testing.T) {
	root := t.TempDir()
	_, aKey, err := volumeOf(root)
	if err != nil {
		t.Fatal(err)
	}
	other := os.Getenv("SystemRoot")
	if other == "" {
		t.Skip("no second volume available")
	}
	_, bKey, err := volumeOf(other)
	if err != nil || aKey == bKey {
		t.Skip("test workspace and system directory share a volume")
	}
	a, err := Acquire(context.Background(), root, Options{Query: plenty})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Acquiring a lease only reads volume metadata, never writes into SystemRoot.
	b, err := Acquire(ctx, other, Options{Query: plenty})
	if err != nil {
		t.Fatalf("unrelated volume was blocked: %v", err)
	}
	b.Close()
}
