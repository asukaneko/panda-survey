package handler

import (
	"net/http"
	"time"

	"panda-survey/internal/auth"
	"panda-survey/internal/middleware"
)

type authReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (d *Deps) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "panda_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil, // HTTPS 下启用 Secure，HTTP 部署不设以免失效
		MaxAge:   int(d.SessionTTL.Seconds()),
	})
}

func (d *Deps) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: "panda_session", Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, MaxAge: -1,
	})
}

func (d *Deps) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req authReq
	if !readJSON(w, r, &req) {
		return
	}
	u, err := d.Auth.Register(req.Username, req.Password)
	if err != nil {
		if err == auth.ErrDuplicateUser {
			fail(w, http.StatusBadRequest, 1001, "用户名已存在")
			return
		}
		fail(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	token, _, err := d.Auth.NewSession(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, "创建会话失败")
		return
	}
	d.setSessionCookie(w, r, token)
	ok(w, u)
}

func (d *Deps) handleLogin(w http.ResponseWriter, r *http.Request) {
	if d.LoginLimit != nil && !d.LoginLimit.Allow(middleware.ClientIP(r)) {
		fail(w, http.StatusTooManyRequests, 1005, "尝试过于频繁，请稍后再试")
		return
	}
	var req authReq
	if !readJSON(w, r, &req) {
		return
	}
	u, err := d.Auth.Login(req.Username, req.Password)
	if err != nil {
		switch err {
		case auth.ErrBadCredentials:
			fail(w, http.StatusUnauthorized, 1001, "用户名或密码错误")
		case auth.ErrLocked:
			fail(w, http.StatusTooManyRequests, 1005, "失败次数过多，账号已临时锁定，请 10 分钟后再试")
		case auth.ErrBanned:
			fail(w, http.StatusForbidden, 1003, "账号已被封禁")
		default:
			fail(w, http.StatusInternalServerError, 1, err.Error())
		}
		return
	}
	token, _, err := d.Auth.NewSession(u.ID)
	if err != nil {
		fail(w, http.StatusInternalServerError, 1, "创建会话失败")
		return
	}
	d.setSessionCookie(w, r, token)
	ok(w, u)
}

func (d *Deps) handleLogout(w http.ResponseWriter, r *http.Request) {
	if u := middleware.User(r); u != nil {
		d.Auth.Logout(u.Token)
	}
	d.clearSessionCookie(w, r)
	ok(w, nil)
}

func (d *Deps) handleMe(w http.ResponseWriter, r *http.Request) {
	u := middleware.User(r)
	ok(w, map[string]any{
		"id": u.UserID, "username": u.Username, "role": u.Role,
	})
}

type changePasswordReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (d *Deps) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req changePasswordReq
	if !readJSON(w, r, &req) {
		return
	}
	u := middleware.User(r)
	if err := d.Auth.ChangePassword(u.UserID, req.OldPassword, req.NewPassword); err != nil {
		if err == auth.ErrBadCredentials {
			fail(w, http.StatusBadRequest, 1001, "旧密码错误")
			return
		}
		fail(w, http.StatusBadRequest, 1001, err.Error())
		return
	}
	d.clearSessionCookie(w, r) // 全部 session 已吊销
	ok(w, map[string]any{"message": "密码已修改，请重新登录", "relogin_at": time.Now().Unix()})
}
