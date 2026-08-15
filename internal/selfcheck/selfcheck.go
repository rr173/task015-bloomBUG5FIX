package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"

	"task015-bloom/internal/bloom"
	"task015-bloom/internal/httpapi"
)

// Run 执行不依赖外部服务的自检：构造过滤器与 HTTP 服务，验证关键边界约束。
// 全部通过返回 0，任一失败返回 1。
func Run() int {
	passed, failed := 0, 0
	check := func(name string, fn func() error) {
		if err := fn(); err != nil {
			failed++
			fmt.Printf("FAIL %-32s %v\n", name, err)
		} else {
			passed++
			fmt.Printf("PASS %s\n", name)
		}
	}

	filter, err := bloom.New(1000, 0.01)
	if err != nil {
		fmt.Printf("FAIL 创建过滤器                %v\n", err)
		return 1
	}
	srv := httptest.NewServer(httpapi.New(filter).Handler())
	defer srv.Close()

	do := func(method, path, body string) (*http.Response, []byte, error) {
		var r io.Reader
		if body != "" {
			r = bytes.NewReader([]byte(body))
		}
		req, err := http.NewRequest(method, srv.URL+path, r)
		if err != nil {
			return nil, nil, err
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, nil, err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, data, readErr
	}

	addBody := func(item string) string {
		b, _ := json.Marshal(map[string]any{"item": item})
		return string(b)
	}
	add := func(item string) error {
		resp, body, err := do(http.MethodPost, "/add", addBody(item))
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("add status=%d body=%s", resp.StatusCode, body)
		}
		return nil
	}
	testReq := func(item string) (bool, error) {
		resp, body, err := do(http.MethodPost, "/test", addBody(item))
		if err != nil {
			return false, err
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("test status=%d body=%s", resp.StatusCode, body)
		}
		var out struct {
			Maybe bool `json:"maybe"`
		}
		if err := json.Unmarshal(body, &out); err != nil {
			return false, err
		}
		return out.Maybe, nil
	}
	statsReq := func() (bloom.Stats, error) {
		resp, body, err := do(http.MethodGet, "/stats", "")
		if err != nil {
			return bloom.Stats{}, err
		}
		if resp.StatusCode != http.StatusOK {
			return bloom.Stats{}, fmt.Errorf("stats status=%d body=%s", resp.StatusCode, body)
		}
		var s bloom.Stats
		if err := json.Unmarshal(body, &s); err != nil {
			return bloom.Stats{}, err
		}
		return s, nil
	}

	check("健康检查", func() error {
		resp, _, err := do(http.MethodGet, "/healthz", "")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("位图长度与哈希个数符合公式", func() error {
		s, err := statsReq()
		if err != nil {
			return err
		}
		if s.M != 9586 {
			return fmt.Errorf("m want 9586, got %d", s.M)
		}
		if s.K != 6 {
			return fmt.Errorf("k want 6, got %d", s.K)
		}
		if s.Capacity != 1000 {
			return fmt.Errorf("capacity want 1000, got %d", s.Capacity)
		}
		if s.FPRate != 0.01 {
			return fmt.Errorf("fp_rate want 0.01, got %v", s.FPRate)
		}
		return nil
	})

	check("空过滤器统计与估算误判率为 0", func() error {
		s, err := statsReq()
		if err != nil {
			return err
		}
		if s.Count != 0 || s.BitsSet != 0 {
			return fmt.Errorf("want empty, got count=%d bits=%d", s.Count, s.BitsSet)
		}
		if s.EstimatedFP != 0 {
			return fmt.Errorf("estimated fp want 0, got %v", s.EstimatedFP)
		}
		return nil
	})

	check("无假阴性：已加入元素恒返回可能存在", func() error {
		for i := 0; i < 1500; i++ { // 远超设计容量 1000
			if err := add("elem-" + itoa(i)); err != nil {
				return err
			}
		}
		for i := 0; i < 1500; i++ {
			maybe, err := testReq("elem-" + itoa(i))
			if err != nil {
				return err
			}
			if !maybe {
				return fmt.Errorf("false negative for elem-%d", i)
			}
		}
		return nil
	})

	check("未加入元素在空状态时一定不存在", func() error {
		// 用一个独立空过滤器验证：未加入的元素必返回 false。
		f2, _ := bloom.New(1000, 0.01)
		s2 := httptest.NewServer(httpapi.New(f2).Handler())
		defer s2.Close()
		req, _ := http.NewRequest(http.MethodPost, s2.URL+"/test", bytes.NewReader([]byte(addBody("never-added"))))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out struct {
			Maybe bool `json:"maybe"`
		}
		_ = json.Unmarshal(body, &out)
		if out.Maybe {
			return fmt.Errorf("empty filter reported maybe for absent item")
		}
		return nil
	})

	check("当前估算误判率由实际填充计算", func() error {
		s, err := statsReq()
		if err != nil {
			return err
		}
		want := math.Pow(float64(s.BitsSet)/float64(s.M), float64(s.K))
		if math.Abs(want-s.EstimatedFP) > 1e-9 {
			return fmt.Errorf("estimated fp want %v, got %v", want, s.EstimatedFP)
		}
		if s.EstimatedFP < 0 || s.EstimatedFP > 1 {
			return fmt.Errorf("estimated fp out of [0,1]: %v", s.EstimatedFP)
		}
		if s.FillRatio != float64(s.BitsSet)/float64(s.M) {
			return fmt.Errorf("fill ratio mismatch: %v", s.FillRatio)
		}
		return nil
	})

	check("加入计数随加入递增且不 panic", func() error {
		s1, _ := statsReq()
		if err := add("count-probe"); err != nil {
			return err
		}
		s2, _ := statsReq()
		if s2.Count != s1.Count+1 {
			return fmt.Errorf("count want %d, got %d", s1.Count+1, s2.Count)
		}
		return nil
	})

	check("重复加入幂等置位", func() error {
		s0, _ := statsReq()
		if err := add("dup-elem"); err != nil {
			return err
		}
		s1, _ := statsReq()
		if err := add("dup-elem"); err != nil {
			return err
		}
		if err := add("dup-elem"); err != nil {
			return err
		}
		s3, _ := statsReq()
		if s3.BitsSet != s1.BitsSet {
			return fmt.Errorf("bits changed after re-add: %d -> %d", s1.BitsSet, s3.BitsSet)
		}
		if s0.BitsSet == s1.BitsSet {
			return fmt.Errorf("first add did not set any bit")
		}
		return nil
	})

	check("删除请求被拒绝", func() error {
		body := addBody("to-delete")
		resp, respBody, err := do(http.MethodPost, "/delete", body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("delete status=%d body=%s", resp.StatusCode, respBody)
		}
		var out struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(respBody, &out)
		if !strings.Contains(out.Error, "删除") {
			return fmt.Errorf("error should mention delete: %s", out.Error)
		}
		return nil
	})

	check("空字符串为合法元素", func() error {
		// item 字段存在但为空字符串，应作为合法元素加入并查询到可能存在。
		if err := add(""); err != nil {
			return err
		}
		maybe, err := testReq("")
		if err != nil {
			return err
		}
		if !maybe {
			return fmt.Errorf("empty-string item not found after add")
		}
		return nil
	})

	check("多段 JSON 请求被拒绝", func() error {
		body := addBody("x") + " {}"
		resp, _, err := do(http.MethodPost, "/add", body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	check("非法 JSON 请求被拒绝", func() error {
		resp, _, err := do(http.MethodPost, "/add", "{not json")
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusBadRequest {
			return fmt.Errorf("status=%d", resp.StatusCode)
		}
		return nil
	})

	fmt.Printf("\n%d passed, %d failed\n", passed, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// itoa 仅供自检输出使用的简易整数转字符串。
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

