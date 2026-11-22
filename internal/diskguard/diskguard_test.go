package diskguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

func occupyVolumeGate(key uint32) *Lease {
	g := &gate{slot: make(chan struct{}, 1), refs: 1}
	g.slot <- struct{}{}
	volumes.Lock()
	volumes.gates[key] = g
	volumes.Unlock()
	return &Lease{ctx: context.Background(), key: key, g: g}
}

func TestDifferentRealVolumesDoNotBlock(t *testing.T) {
	root := t.TempDir()
	_, aKey, err := volumeOf(root)
	if err != nil {
		t.Fatal(err)
	}
	// 1) 单分区机器上也不跳过：先在全局卷门闸里占住别的卷号（含 0，防止实现把所有卷号都退化成 0），验证 root 所在卷不会被挡住
	otherKeys := []uint32{aKey ^ 1}
	if aKey != 0 {
		otherKeys = append(otherKeys, 0)
	}
	var holds []*Lease
	for _, k := range otherKeys {
		h := occupyVolumeGate(k)
		holds = append(holds, h)
		defer h.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waited := false
	a, err := Acquire(ctx, root, Options{Query: plenty, OnWait: func() { waited = true }})
	if err != nil {
		t.Fatalf("unrelated volume gate blocked current volume: %v", err)
	}
	if waited {
		a.Close()
		t.Fatal("unrelated volume gate caused current volume to queue")
	}
	for _, h := range holds {
		h.Close()
	}

	// 2) 机器上真有第二个挂载卷时（如多盘符开发机或带 D: 的 CI runner），再用两个真实目录端到端验证互不阻塞
	var cands []string
	if wd, err := os.Getwd(); err == nil && wd != "" {
		cands = append(cands, wd)
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		cands = append(cands, sr)
	}
	if runtime.GOOS == "windows" {
		for d := byte('C'); d <= 'Z'; d++ {
			cands = append(cands, string([]byte{d, ':', '\\'}))
		}
	} else {
		cands = append(cands, "/", "/dev", "/tmp")
	}
	second := ""
	for _, c := range cands {
		if _, bKey, err := volumeOf(c); err == nil && bKey != aKey {
			second = c
			break
		}
	}
	if second != "" {
		rctx, rcancel := context.WithTimeout(context.Background(), time.Second)
		defer rcancel()
		b, err := Acquire(rctx, second, Options{Query: plenty})
		if err != nil {
			a.Close()
			t.Fatalf("unrelated real volume %q was blocked: %v", second, err)
		}
		b.Close()
	} else {
		t.Log("本机只有一个真实挂载卷，真实双卷分支未触发（已通过不同卷键门闸验证互不阻塞）")
	}
	a.Close()

	volumes.Lock()
	left := len(volumes.gates)
	volumes.Unlock()
	if left != 0 {
		t.Fatalf("leases leaked %d volume records", left)
	}
}
