package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Host, Port, AppURL, DataDir, FilesDir, FrontendDir            string
	MySQLHost, MySQLPort, MySQLUser, MySQLPassword, MySQLDatabase string
	RedisAddr, RedisPassword, RedisPrefix                         string
	RedisDB                                                       int
	Production, Demo, CookieSecure                                bool
	Key                                                           []byte
	OSS                                                           OSSConfig
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
func LoadConfig() (Config, error) {
	_ = godotenv.Load()
	c := Config{Host: env("HOST", "127.0.0.1"), Port: env("PORT", "3001"), AppURL: env("APP_URL", "http://127.0.0.1:5173"), DataDir: env("DATA_DIR", "data"), FrontendDir: env("FRONTEND_DIR", "dist"), MySQLHost: env("MYSQL_HOST", "127.0.0.1"), MySQLPort: env("MYSQL_PORT", "3307"), MySQLUser: env("MYSQL_USER", "phantomframe"), MySQLPassword: env("MYSQL_PASSWORD", ""), MySQLDatabase: env("MYSQL_DATABASE", "phantomframe"), RedisAddr: env("REDIS_ADDR", "127.0.0.1:6380"), RedisPassword: env("REDIS_PASSWORD", ""), RedisPrefix: env("REDIS_PREFIX", "phantomframe:"), Production: env("APP_ENV", env("NODE_ENV", "development")) == "production"}
	c.RedisDB, _ = strconv.Atoi(env("REDIS_DB", "0"))
	c.Demo = env("DEMO_MODE", strconv.FormatBool(!c.Production)) == "true"
	c.CookieSecure = env("COOKIE_SECURE", strconv.FormatBool(c.Production)) == "true"
	c.OSS = defaultOSSConfig(c.Production)
	c.DataDir, _ = filepath.Abs(c.DataDir)
	c.FilesDir = filepath.Join(c.DataDir, "files")
	if err := os.MkdirAll(c.FilesDir, 0700); err != nil {
		return c, err
	}
	key := env("ENCRYPTION_KEY", "")
	if key == "" {
		if c.Production {
			return c, fmt.Errorf("production requires ENCRYPTION_KEY")
		}
		path := filepath.Join(c.DataDir, ".encryption-key")
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			key = randomHex(32)
			e = os.WriteFile(path, []byte(key), 0600)
		} else {
			key = strings.TrimSpace(string(b))
		}
		if e != nil {
			return c, e
		}
	}
	var err error
	c.Key, err = hex.DecodeString(key)
	if err != nil || len(c.Key) != 32 {
		return c, fmt.Errorf("ENCRYPTION_KEY must contain 64 hex characters")
	}
	return c, nil
}
func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return b
}
func randomHex(n int) string { return hex.EncodeToString(randomBytes(n)) }
func token() string          { return base64.RawURLEncoding.EncodeToString(randomBytes(32)) }
func id() string {
	b := randomBytes(16)
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func (a *App) encrypt(s string) string {
	if s == "" {
		return ""
	}
	block, e := aes.NewCipher(a.Config.Key)
	must(e)
	g, e := cipher.NewGCM(block)
	must(e)
	iv := randomBytes(g.NonceSize())
	data := g.Seal(nil, iv, []byte(s), nil)
	n := len(data) - g.Overhead()
	enc := base64.StdEncoding.EncodeToString
	return enc(iv) + "." + enc(data[n:]) + "." + enc(data[:n])
}
func (a *App) decrypt(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid encrypted key")
	}
	values := make([][]byte, 3)
	for i, p := range parts {
		b, e := base64.StdEncoding.DecodeString(p)
		if e != nil {
			return "", e
		}
		values[i] = b
	}
	block, e := aes.NewCipher(a.Config.Key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	if len(values[0]) != g.NonceSize() {
		return "", fmt.Errorf("invalid nonce")
	}
	b, e := g.Open(nil, values[0], append(values[2], values[1]...), nil)
	return string(b), e
}
