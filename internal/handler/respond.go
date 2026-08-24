package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"panda-survey/internal/store"
)

// 统一响应与错误码：
// 0 成功；1001 参数/校验错误；1002 未登录；1003 无权限；1004 问卷状态不允许；
// 1005 限频或配额用尽；1006 AI 未配置或调用失败；1007 乐观锁冲突；1008 资源不存在。

func ok(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok", "data": data})
}

func fail(w http.ResponseWriter, httpStatus, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": msg, "data": nil})
}

// mapErr 把领域错误映射为统一的 HTTP 状态与业务码。
func mapErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, 1008, "资源不存在")
	case errors.Is(err, store.ErrForbidden):
		fail(w, http.StatusForbidden, 1003, "无权操作他人资源")
	case errors.Is(err, store.ErrConflict):
		fail(w, http.StatusConflict, 1007, "数据已被修改，请刷新后重试")
	case errors.Is(err, store.ErrState):
		fail(w, http.StatusBadRequest, 1004, err.Error())
	case errors.Is(err, store.ErrSurveyClosed):
		fail(w, http.StatusBadRequest, 1004, "问卷已停止回收")
	default:
		fail(w, http.StatusBadRequest, 1001, err.Error())
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		fail(w, http.StatusBadRequest, 1001, "读取请求体失败")
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		fail(w, http.StatusBadRequest, 1001, "请求体不是合法 JSON")
		return false
	}
	return true
}
