// Package httpapi 提供布隆过滤器服务的 HTTP 接口。
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"task015-bloom/internal/bloom"
)

// ErrBadJSON 表示请求体不是单个合法 JSON 对象。
var ErrBadJSON = errors.New("请求体不是合法的单个 JSON 对象")

// API 是布隆过滤器服务的 HTTP 接口实现，封装一个全局过滤器实例。
type API struct {
	filter *bloom.Filter
}

// New 创建使用给定过滤器的服务实例。
func New(filter *bloom.Filter) *API {
	return &API{filter: filter}
}

// Handler 返回 HTTP 路由。
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /add", a.add)
	mux.HandleFunc("POST /test", a.test)
	mux.HandleFunc("GET /stats", a.stats)
	mux.HandleFunc("POST /delete", a.delete)
	return mux
}

func decodeJSON(r *http.Request, v any) error {
	if r.Body == nil {
		return ErrBadJSON
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadJSON, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return ErrBadJSON
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// itemRequest 使用指针字段以区分“字段缺失”与“字段为空字符串”：
// nil 表示 JSON 中缺失 item 字段；空字符串视为合法的元素值。
type itemRequest struct {
	Item *string `json:"item"`
}

func (a *API) add(w http.ResponseWriter, r *http.Request) {
	var req itemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": http.StatusBadRequest})
		return
	}
	a.filter.Add(*req.Item)
	s := a.filter.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"added":    true,
		"count":    s.Count,
		"bits_set": s.BitsSet,
	})
}

func (a *API) test(w http.ResponseWriter, r *http.Request) {
	var req itemRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "status": http.StatusBadRequest})
		return
	}
	if req.Item == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "缺少 item 字段", "status": http.StatusBadRequest})
		return
	}
	maybe := a.filter.Test(*req.Item)
	s := a.filter.Stats()
	writeJSON(w, http.StatusOK, map[string]any{
		"maybe":        maybe,
		"count":        s.Count,
		"bits_set":     s.BitsSet,
		"estimated_fp": s.EstimatedFP,
	})
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.filter.Stats())
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	// 标准布隆过滤器不支持删除：按位清除会破坏无假阴性保证、引入假阴性。
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error":  "布隆过滤器不支持删除元素",
		"status": http.StatusBadRequest,
	})
}
