// Package bloom 实现一个标准布隆过滤器：由期望容量与期望误判率确定位图长度与
// 哈希个数，支持加入、查询与统计。仅使用标准库，不引入第三方依赖。
package bloom

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sync"
)

// 设计参数非法时返回的错误。调用方可通过 errors.Is 精确区分。
var (
	ErrInvalidCapacity = errors.New("bloom: 期望容量必须为正整数")
	ErrInvalidFPRate   = errors.New("bloom: 期望误判率必须严格介于 0 与 1 之间")
)

var ln2 = math.Log(2)

// Filter 是一个并发安全的布隆过滤器。
// 位图以字节切片按位打包存储；count 记录加入操作的次数（含重复加入同一元素），
// bitsSet 记录当前已置为 1 的位位数。
type Filter struct {
	mu      sync.RWMutex
	bits    []byte
	m       uint64  // 位图长度（位数）
	k       uint64  // 哈希函数个数
	capcity uint64  // 设计期望容量 n
	fpRate  float64 // 设计期望误判率 p
	count   uint64  // 已加入次数 N
	bitsSet uint64  // 已置位位数 X
}

// New 根据期望容量 n 与期望误判率 p 创建过滤器。
// n 必须为正整数，p 必须严格介于 0 与 1 之间，否则返回错误。
// 位图长度 m 与哈希个数 k 由标准解析公式计算得出。
func New(capacity uint64, fpRate float64) (*Filter, error) {
	if capacity == 0 {
		return nil, ErrInvalidCapacity
	}
	// 显式排除 NaN 与越界取值：p 必须严格落在 (0,1)。
	if !(fpRate >= 0 && fpRate < 1) {
		return nil, ErrInvalidFPRate
	}
	m := OptimalM(capacity, fpRate)
	k := OptimalK(m, capacity)
	return &Filter{
		bits:    make([]byte, (m+7)/8),
		m:       m,
		k:       k,
		capcity: capacity,
		fpRate:  fpRate,
	}, nil
}

// OptimalM 由期望容量 n 与期望误判率 p 计算位图长度：
// m = ⌈-(n·ln p)/(ln 2)²⌉，向上取整。
func OptimalM(n uint64, p float64) uint64 {
	m := -float64(n) * math.Log(p) / (ln2 * ln2)
	return uint64(math.Ceil(m))
}

// OptimalK 由位图长度 m 与期望容量 n 计算哈希个数：
// k = max(1, round((m/n)·ln 2))，四舍五入到最近整数且不小于 1。
func OptimalK(m, n uint64) uint64 {
	if n == 0 {
		return 1
	}
	k := float64(m) / float64(n) * ln2
	r := math.Floor(k)
	if r < 1 {
		r = 1
	}
	return uint64(r)
}

// Add 将元素加入过滤器：对其 k 个哈希位置全部置 1，并把加入计数加 1。
// 重复加入同一元素不会改变已置位位数（幂等置位），但加入计数仍会递增。
func (f *Filter) Add(item string) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, p := range f.positions(item) {
		byteIdx := p / 8
		bitIdx := p % 8
		mask := byte(1) << bitIdx
		if f.bits[byteIdx]&mask == 0 {
			f.bits[byteIdx] |= mask
			f.bitsSet++
		}
	}
	f.count++
}

// Test 查询元素是否可能存在：若 k 个哈希位置均为 1 返回 true（可能存在），
// 任一位置为 0 返回 false（一定不存在）。已加入元素恒返回 true（无假阴性）。
func (f *Filter) Test(item string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, p := range f.positions(item) {
		if f.bits[p/8]&(byte(1)<<(p%8)) == 0 {
			return false
		}
	}
	return true
}

// positions 返回元素经双哈希派生出的 k 个位位置，均落在 [0, m) 区间。
// 以 SHA-256 摘要的前 16 字节拆分为两个 64 位基哈希 h1、h2，第 i 个位置为
// (h1 + i·h2) mod m（Kirsch-Mitzenmacher 双哈希），无需对每个位置独立计算完整哈希。
func (f *Filter) positions(item string) []uint64 {
	sum := sha256.Sum256([]byte(item))
	h1 := binary.BigEndian.Uint64(sum[0:8])
	h2 := binary.BigEndian.Uint64(sum[8:16])
	m := f.m
	h1 %= m
	h2 %= m
	pos := make([]uint64, f.k)
	acc := h1 + h2
	for i := uint64(0); i < f.k; i++ {
		pos[i] = acc % m
		acc += h2 // 累加 h2 等价于 (h1 + i·h2) mod m；m 受容量约束远小于 2^64，不会溢出
	}
	return pos
}

// Stats 是过滤器的统计快照。
type Stats struct {
	M           uint64  `json:"m"`            // 位图长度
	K           uint64  `json:"k"`            // 哈希个数
	Capacity    uint64  `json:"capacity"`     // 设计期望容量
	FPRate      float64 `json:"fp_rate"`      // 设计期望误判率
	Count       uint64  `json:"count"`        // 已加入次数
	BitsSet     uint64  `json:"bits_set"`     // 已置位位数
	FillRatio   float64 `json:"fill_ratio"`   // 填充率 = 已置位 / 位图长度
	EstimatedFP float64 `json:"estimated_fp"` // 当前估算误判率 = 填充率的 k 次方
}

// Stats 返回当前统计。当前估算误判率由实际位图填充率与哈希个数实时计算：
// estimated_fp = (已置位数 / 位图长度)^哈希个数；空过滤器为 0。
func (f *Filter) Stats() Stats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var est float64
	if f.m > 0 {
		ratio := float64(f.bitsSet) / float64(f.m)
		est = math.Pow(ratio, float64(f.k))
	}
	return Stats{
		M:           f.m,
		K:           f.k,
		Capacity:    f.capcity,
		FPRate:      f.fpRate,
		Count:       f.count,
		BitsSet:     f.bitsSet,
		FillRatio:   float64(f.bitsSet) / float64(f.m),
		EstimatedFP: est,
	}
}
