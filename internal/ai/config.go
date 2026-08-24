package ai

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"panda-survey/internal/store"
)

const settingsKey = "ai_config"

// Config AI 服务配置；API Key 落库时按 SecretKey 加密。
type Config struct {
	BaseURL    string `json:"base_url"`
	APIKey     string `json:"api_key"`
	Model      string `json:"model"`
	TimeoutSec int    `json:"timeout_sec"`
	DailyQuota int    `json:"daily_quota"`
}

var ErrNotConfigured = errors.New("AI 服务未配置，请联系管理员在设置页配置")

func DefaultConfig() Config {
	return Config{TimeoutSec: 60, DailyQuota: 50}
}

// Load 从 settings 读取配置并解密 API Key；未配置时返回 ErrNotConfigured。
func Load(st *store.SettingsStore, secretKey string) (Config, error) {
	cfg := DefaultConfig()
	raw, ok, err := st.Get(settingsKey)
	if err != nil {
		return cfg, err
	}
	if !ok {
		return cfg, ErrNotConfigured
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return DefaultConfig(), fmt.Errorf("AI 配置损坏: %w", err)
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 60
	}
	if cfg.DailyQuota <= 0 {
		cfg.DailyQuota = 50
	}
	key, err := decryptMaybe(secretKey, cfg.APIKey)
	if err != nil {
		return cfg, err
	}
	cfg.APIKey = key
	if cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "" {
		return cfg, ErrNotConfigured
	}
	return cfg, nil
}

// Save 加密 API Key 后写入 settings。
func Save(st *store.SettingsStore, secretKey string, cfg Config) error {
	toStore := cfg
	enc, err := encryptMaybe(secretKey, cfg.APIKey)
	if err != nil {
		return err
	}
	toStore.APIKey = enc
	b, err := json.Marshal(toStore)
	if err != nil {
		return err
	}
	return st.Set(settingsKey, string(b))
}

// Masked 返回给前端的视图：Key 只留尾 4 位。
func Masked(cfg Config) Config {
	m := cfg
	if len(m.APIKey) > 4 {
		m.APIKey = "****" + m.APIKey[len(m.APIKey)-4:]
	} else if m.APIKey != "" {
		m.APIKey = "****"
	}
	return m
}

// KeyChanged 前端提交 "****xxxx" 形式的掩码表示未修改 Key。
func KeyMasked(v string) bool { return strings.HasPrefix(v, "****") }

// ---- AES-GCM 加密：密文格式 enc:<base64(nonce|data)> ----

func encryptMaybe(secret, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if secret == "" {
		return "plain:" + plain, nil // 未设 SECRET_KEY：明文存储并带前缀
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptMaybe(secret, stored string) (string, error) {
	switch {
	case stored == "":
		return "", nil
	case strings.HasPrefix(stored, "enc:"):
		if secret == "" {
			return "", fmt.Errorf("API Key 为密文但未设置 SECRET_KEY，无法解密")
		}
		key := sha256.Sum256([]byte(secret))
		block, err := aes.NewCipher(key[:])
		if err != nil {
			return "", err
		}
		gcm, err := cipher.NewGCM(block)
		if err != nil {
			return "", err
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, "enc:"))
		if err != nil {
			return "", fmt.Errorf("API Key 密文损坏")
		}
		if len(raw) < gcm.NonceSize() {
			return "", fmt.Errorf("API Key 密文损坏")
		}
		plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
		if err != nil {
			return "", fmt.Errorf("API Key 解密失败（SECRET_KEY 不匹配）")
		}
		return string(plain), nil
	case strings.HasPrefix(stored, "plain:"):
		return strings.TrimPrefix(stored, "plain:"), nil
	default:
		return stored, nil // 兼容历史明文
	}
}
