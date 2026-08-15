package bloom

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestNewRejectsInvalidCapacity(t *testing.T) {
	cases := []uint64{0}
	for _, n := range cases {
		if _, err := New(n, 0.01); !errors.Is(err, ErrInvalidCapacity) {
			t.Fatalf("n=%d want ErrInvalidCapacity, got %v", n, err)
		}
	}
}

func TestNewRejectsInvalidFPRate(t *testing.T) {
	cases := []float64{-0.1, 1, 1.5, 2, math.NaN()}
	for _, p := range cases {
		if _, err := New(1000, p); !errors.Is(err, ErrInvalidFPRate) {
			t.Fatalf("p=%v want ErrInvalidFPRate, got %v", p, err)
		}
	}
}

func TestOptimalMAndK(t *testing.T) {
	// n=1000, p=0.01：m = ⌈-(1000·ln0.01)/(ln2)²⌉ = 9586，k = floor((9586/1000)·ln2) = 6。
	f, err := New(1000, 0.01)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if f.m != 9586 {
		t.Fatalf("m want 9586, got %d", f.m)
	}
	if f.k != 6 {
		t.Fatalf("k want 6, got %d", f.k)
	}
	if len(f.bits) != (9586+7)/8 {
		t.Fatalf("bits len want %d, got %d", (9586+7)/8, len(f.bits))
	}
}

func TestOptimalMAndKSecondCase(t *testing.T) {
	// n=100, p=0.05：m = ⌈-(100·ln0.05)/(ln2)²⌉ = 624，k = round((624/100)·ln2) = 4。
	f, err := New(100, 0.05)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if f.m != 624 {
		t.Fatalf("m want 624, got %d", f.m)
	}
	if f.k != 4 {
		t.Fatalf("k want 4, got %d", f.k)
	}
}

func TestNoFalseNegatives(t *testing.T) {
	f, _ := New(1000, 0.01)
	items := make([]string, 2000) // 远超设计容量
	for i := range items {
		items[i] = "item-" + strconv.Itoa(i)
	}
	for _, it := range items {
		f.Add(it)
	}
	// 加入全部后查询每一个已加入元素，必须全部可能存在。
	for _, it := range items {
		if !f.Test(it) {
			t.Fatalf("false negative for %q", it)
		}
	}
}

func TestAbsentItemMayBeAbsent(t *testing.T) {
	// 未加入的元素必须返回 false（一定不存在）——空过滤器对任意元素都返回 false。
	f, _ := New(1000, 0.01)
	for i := 0; i < 100; i++ {
		if f.Test("absent-" + strconv.Itoa(i)) {
			t.Fatalf("empty filter reported maybe for absent item %d", i)
		}
	}
}

func TestAddIdempotentBitsSet(t *testing.T) {
	// 重复加入同一元素不应改变已置位位数（幂等置位），但加入计数递增。
	f, _ := New(1000, 0.01)
	f.Add("dup")
	after1 := f.Stats()
	f.Add("dup")
	f.Add("dup")
	after3 := f.Stats()
	if after3.BitsSet != after1.BitsSet {
		t.Fatalf("bits set changed after re-add: %d -> %d", after1.BitsSet, after3.BitsSet)
	}
	if after3.Count != 3 {
		t.Fatalf("count want 3, got %d", after3.Count)
	}
}

func TestStatsEstimatedFP(t *testing.T) {
	// 当前估算误判率必须等于 (已置位数/位图长度)^哈希个数；空过滤器为 0。
	f, _ := New(1000, 0.01)
	s0 := f.Stats()
	if s0.EstimatedFP != 0 {
		t.Fatalf("empty estimated fp want 0, got %v", s0.EstimatedFP)
	}
	for i := 0; i < 500; i++ {
		f.Add("x-" + strconv.Itoa(i))
	}
	s := f.Stats()
	want := math.Pow(float64(s.BitsSet)/float64(s.M), float64(s.K))
	if math.Abs(want-s.EstimatedFP) > 1e-12 {
		t.Fatalf("estimated fp want %v, got %v", want, s.EstimatedFP)
	}
	if s.EstimatedFP < 0 || s.EstimatedFP > 1 {
		t.Fatalf("estimated fp out of [0,1]: %v", s.EstimatedFP)
	}
	if s.FillRatio != float64(s.BitsSet)/float64(s.M) {
		t.Fatalf("fill ratio mismatch: %v", s.FillRatio)
	}
}

func TestStatsBeyondCapacity(t *testing.T) {
	// 插入数远超设计容量时仍须保持无假阴性、统计正确且不 panic，仅误判率升高。
	f, _ := New(100, 0.01)
	for i := 0; i < 5000; i++ {
		f.Add("over-" + strconv.Itoa(i))
	}
	for i := 0; i < 5000; i++ {
		if !f.Test("over-" + strconv.Itoa(i)) {
			t.Fatalf("false negative beyond capacity for %d", i)
		}
	}
	s := f.Stats()
	if s.Count != 5000 {
		t.Fatalf("count want 5000, got %d", s.Count)
	}
	if s.BitsSet > s.M {
		t.Fatalf("bits set %d exceeds m %d", s.BitsSet, s.M)
	}
}
