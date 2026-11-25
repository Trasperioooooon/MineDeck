package diskguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	// 2) 候选目录只留工作目录和 SystemRoot，用不经过 volumeOf 的系统调用判断是不是在另一个卷上；
	// 序列号不同时先断言 volumeOf 给两边的卷号也不同，再验证跨卷 Acquire 互不阻塞。
	var cands []string
	if wd, err := os.Getwd(); err == nil && wd != "" {
		cands = append(cands, wd)
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		cands = append(cands, sr)
	}
	second := ""
	var secondSerial uint32
	rootSerial, rootOK := testVolumeSerial(root)
	if rootOK {
		for _, c := range cands {
			if s, ok := testVolumeSerial(c); ok && s != rootSerial {
				second, secondSerial = c, s
				break
			}
		}
	}
	if second != "" {
		_, bKey, err := volumeOf(second)
		if err != nil {
			a.Close()
			t.Fatalf("volumeOf(%q): %v", second, err)
		}
		if bKey == aKey {
			a.Close()
			t.Fatalf("不同卷（序列号 %#x 与 %#x）被 volumeOf 算成了同一个卷号 %#x", rootSerial, secondSerial, aKey)
		}
		rctx, rcancel := context.WithTimeout(context.Background(), time.Second)
		defer rcancel()
		bWaited := false
		b, err := Acquire(rctx, second, Options{Query: plenty, OnWait: func() { bWaited = true }})
		if err != nil {
			a.Close()
			t.Fatalf("unrelated real volume %q was blocked: %v", second, err)
		}
		if bWaited {
			b.Close()
			a.Close()
			t.Fatalf("unrelated real volume %q queued behind current volume", second)
		}
		b.Close()
		t.Logf("第二段已运行：两个真实卷（序列号 %#x 与 %#x）volumeOf 卷号不同（%#x vs %#x）且 Acquire 互不阻塞", rootSerial, secondSerial, aKey, bKey)
	} else {
		t.Log("这台机器上工作目录和系统目录在同一个卷（或者查不到卷序列号），第二段没跑")
	}
	a.Close()

	volumes.Lock()
	left := len(volumes.gates)
	volumes.Unlock()
	if left != 0 {
		t.Fatalf("leases leaked %d volume records", left)
	}
}

func TestNoSpaceHint(t *testing.T) {
	e := &NoSpace{Need: 1 << 30, Free: 128 << 20}
	if got := e.Hint(); !strings.HasPrefix(got, "磁盘剩余空间不足") {
		t.Fatalf("默认 Hint 不符：%q", got)
	}
	withVol := &NoSpace{Need: 1 << 30, Free: 128 << 20, Volume: "D 盘"}
	if got := withVol.Hint(); !strings.HasPrefix(got, "D 盘剩余空间不足") {
		t.Fatalf("带盘符 Hint 不符：%q", got)
	}
}
