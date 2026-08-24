package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"panda-survey/internal/model"
)

var (
	ErrBadCredentials = errors.New("用户名或密码错误")
	ErrLocked         = errors.New("失败次数过多，账号已临时锁定")
	ErrDuplicateUser  = errors.New("用户名已存在")
	ErrBanned         = errors.New("账号已被封禁")
)

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_]{3,24}$`)

const (
	maxFailures = 5
	lockWindow  = 10 * time.Minute
)

type failState struct {
	count int
	until time.Time
}

type Service struct {
	db  *sql.DB
	ttl time.Duration

	mu      sync.Mutex
	failures map[string]*failState
}

func New(db *sql.DB, ttl time.Duration) *Service {
	return &Service{db: db, ttl: ttl, failures: map[string]*failState{}}
}

// ValidateCredentials 用户名 3-24 位字母数字下划线，密码至少 8 位。
func ValidateCredentials(username, password string) error {
	if !usernameRe.MatchString(username) {
		return fmt.Errorf("用户名须为 3 到 24 位字母、数字或下划线")
	}
	if len(password) < 8 {
		return fmt.Errorf("密码至少 8 位")
	}
	if len(password) > 72 {
		return fmt.Errorf("密码过长")
	}
	return nil
}

func (s *Service) Register(username, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if err := ValidateCredentials(username, password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	role := 0
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		role = 1 // 首个注册用户自动成为管理员
	}
	now := model.NowUTC()
	res, err := s.db.Exec(
		`INSERT INTO users(username, password_hash, role, created_at) VALUES (?,?,?,?)`,
		username, string(hash), role, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDuplicateUser
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.User{ID: id, Username: username, Role: role, CreatedAt: now}, nil
}

func (s *Service) Login(username, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if err := s.lockCheck(username); err != nil {
		return nil, err
	}
	var u model.User
	var hash string
	var status int
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, role, status, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.ID, &u.Username, &hash, &u.Role, &status, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		s.recordFailure(username)
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, ErrBanned
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.recordFailure(username)
		return nil, ErrBadCredentials
	}
	s.resetFailures(username)
	return &u, nil
}

func (s *Service) lockCheck(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs, ok := s.failures[username]
	if !ok {
		return nil
	}
	if fs.count >= maxFailures && time.Now().Before(fs.until) {
		return ErrLocked
	}
	if !fs.until.IsZero() && time.Now().After(fs.until) {
		delete(s.failures, username) // 锁定过期自动解除
	}
	return nil
}

func (s *Service) recordFailure(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fs := s.failures[username]
	if fs == nil {
		fs = &failState{}
		s.failures[username] = fs
	}
	fs.count++
	if fs.count >= maxFailures {
		fs.until = time.Now().Add(lockWindow)
	}
}

func (s *Service) resetFailures(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, username)
}

// ---- Session ----

func (s *Service) NewSession(userID int64) (token string, expiresAt time.Time, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(b)
	expiresAt = time.Now().Add(s.ttl)
	now := model.NowUTC()
	_, err = s.db.Exec(
		`INSERT INTO sessions(token, user_id, expires_at, created_at) VALUES (?,?,?,?)`,
		token, userID, expiresAt.UTC().Format(time.RFC3339Nano), now)
	// 顺手清理过期 session，避免表膨胀
	s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
	return token, expiresAt, nil
}

// UserBySession 校验 token，返回用户；封禁账号视为未登录。
func (s *Service) UserBySession(token string) (*model.User, error) {
	if token == "" {
		return nil, errors.New("无 session")
	}
	var u model.User
	var status int
	var expires string
	err := s.db.QueryRow(
		`SELECT u.id, u.username, u.role, u.status, u.created_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.token = ?`, token,
	).Scan(&u.ID, &u.Username, &u.Role, &status, &u.CreatedAt, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("session 无效")
	}
	if err != nil {
		return nil, err
	}
	exp, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().After(exp) {
		return nil, errors.New("session 过期")
	}
	if status != 0 {
		return nil, ErrBanned
	}
	return &u, nil
}

func (s *Service) Logout(token string) {
	s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
}

// ChangePassword 验证旧密码后更新，并吊销该用户全部 session。
func (s *Service) ChangePassword(userID int64, oldPw, newPw string) error {
	if len(newPw) < 8 || len(newPw) > 72 {
		return fmt.Errorf("新密码至少 8 位")
	}
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPw)) != nil {
		return ErrBadCredentials
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(newHash), userID); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

// PromoteAdmins 启动时按环境变量提升管理员。
func (s *Service) PromoteAdmins(names []string) {
	for _, n := range names {
		s.db.Exec(`UPDATE users SET role = 1 WHERE username = ?`, n)
	}
}
