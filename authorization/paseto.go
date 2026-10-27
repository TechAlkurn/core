package authorization

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"app/pkg/application/common/model"
	"app/pkg/config"
	"app/pkg/core/db/activerecord"
	"app/pkg/enum"

	"aidanwoods.dev/go-paseto"
	"github.com/TechAlkurn/core/lib"
)

// DefaultPaseto is the singleton PASETO service loaded from PASETO_KEY env var.
// Use this for all token operations to ensure consistent key usage.
var DefaultPaseto PasetoService

func init() {
	hexKey := os.Getenv("PASETO_KEY")
	if hexKey == "" {
		//	config.Log.Println("[WARN] PASETO_KEY not set — generating ephemeral key (tokens will not survive restarts)")
		DefaultPaseto = NewPasetoService()
		return
	}
	svc, err := NewPasetoMakerFromHex(hexKey)
	if err != nil {
		config.Log.Fatalf("PASETO_KEY invalid: %v", err)
	}
	DefaultPaseto = svc
}

const (
	AccessTokenDuration  = 24 * time.Hour
	RefreshTokenDuration = 7 * 24 * time.Hour
)

type PasetoService interface {
	CreateToken(claims Claims, duration time.Duration) (string, error)
	VerifyToken(tokenStr string) error
	GenerateToken(entity *model.User) (*tokenPairResponse, error)
	RevokeToken(tokenKey string) error
}

// Claims holds the data embedded in every token.
type Claims struct {
	Id       uint32 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Token    string `json:"token"` // jti — matches user_token.token_key
}

// pasetoService manages PASETO v4 local (symmetric) tokens.
type pasetoService struct {
	symmetricKey paseto.V4SymmetricKey
}

// NewPasetoService generates a fresh random symmetric key (dev/testing only).
func NewPasetoService() PasetoService {
	return &pasetoService{symmetricKey: paseto.NewV4SymmetricKey()}
}

// NewPasetoMakerFromHex loads an existing key from a 64-char hex string.
func NewPasetoMakerFromHex(hexKey string) (*pasetoService, error) {
	key, err := paseto.V4SymmetricKeyFromHex(hexKey)
	if err != nil {
		return nil, fmt.Errorf("invalid hex key: %w", err)
	}
	return &pasetoService{symmetricKey: key}, nil
}

// CreateToken mints a new encrypted PASETO v4 local token.
func (m *pasetoService) CreateToken(claims Claims, duration time.Duration) (string, error) {
	tok := paseto.NewToken()
	tok.SetIssuedAt(time.Now())
	tok.SetNotBefore(time.Now())
	tok.SetExpiration(time.Now().Add(duration))
	if err := tok.Set("user_id", claims.Id); err != nil {
		return "", fmt.Errorf("set user_id: %w", err)
	}
	if err := tok.Set("username", claims.Username); err != nil {
		return "", fmt.Errorf("set username: %w", err)
	}
	if err := tok.Set("email", claims.Email); err != nil {
		return "", fmt.Errorf("set email: %w", err)
	}
	if err := tok.Set("token", claims.Token); err != nil {
		return "", fmt.Errorf("set token: %w", err)
	}
	return tok.V4Encrypt(m.symmetricKey, nil), nil
}

// VerifyToken decrypts and validates a Bearer token, then checks user_token table for revocation.
func (m *pasetoService) VerifyToken(tokenStr string) error {
	parts := strings.SplitN(tokenStr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return errors.New(enum.BearerTokenFormat)
	}
	tokenStr = strings.TrimSpace(parts[1])
	parser := paseto.NewParser()
	parser.AddRule(paseto.NotExpired())
	parser.AddRule(paseto.ValidAt(time.Now()))
	tok, err := parser.ParseV4Local(m.symmetricKey, tokenStr, nil)
	if err != nil {
		return errors.New(enum.ErrInvalidToken)
	}
	_, err = m._claims(tok)
	return err
}

type tokenPairResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// GenerateToken issues a new access+refresh token pair and records the session in user_token.
func (m *pasetoService) GenerateToken(entity *model.User) (*tokenPairResponse, error) {
	tokenKey := lib.GenerateRandomKey(32)
	expiresAt := uint32(time.Now().Add(RefreshTokenDuration).Unix())
	createdAt := uint32(time.Now().Unix())

	base := Claims{
		Id:       entity.Id,
		Username: entity.Username,
		Email:    entity.Email,
		Token:    tokenKey,
	}
	accessToken, err := m.CreateToken(base, AccessTokenDuration)
	if err != nil {
		return nil, err
	}
	refreshToken, err := m.CreateToken(base, RefreshTokenDuration)
	if err != nil {
		return nil, err
	}

	// Register this session — one row per device/login
	config.DB.Debug().Exec(
		"INSERT INTO `user_token` (`user_id`, `token_key`, `created_at`, `expires_at`) VALUES (?, ?, ?, ?)",
		entity.Id, tokenKey, createdAt, expiresAt,
	)

	return &tokenPairResponse{
		Token:        accessToken,
		RefreshToken: refreshToken,
	}, nil
}

// RevokeToken deletes the specific session from user_token — only that device is logged out.
func (m *pasetoService) RevokeToken(tokenKey string) error {
	result := config.DB.Exec("DELETE FROM `user_token` WHERE `token_key` = ?", tokenKey)
	if result.RowsAffected == 0 {
		return errors.New(enum.TokenNotFoundOrRevoked)
	}
	return nil
}

// KeyHex returns the symmetric key as hex for persistence.
func (m *pasetoService) KeyHex() string {
	return m.symmetricKey.ExportHex()
}

func (m *pasetoService) _claims(tok *paseto.Token) (*Claims, error) {
	var userID uint32
	var username, email, tokenKey string

	if err := tok.Get("user_id", &userID); err != nil {
		return nil, errors.New(enum.ErrInvalidToken)
	}
	if err := tok.Get("username", &username); err != nil {
		return nil, errors.New(enum.ErrInvalidToken)
	}
	if err := tok.Get("email", &email); err != nil {
		return nil, errors.New(enum.ErrInvalidToken)
	}
	if err := tok.Get("token", &tokenKey); err != nil {
		return nil, errors.New(enum.ErrInvalidToken)
	}

	// Check session is active (not revoked and not expired)
	if !activerecord.IsDebug() {
		now := uint32(time.Now().Unix())
		isExist := (&model.UserToken{}).IsExist("token_key = ? AND user_id = ? AND expires_at > ?", tokenKey, userID, now)
		if !isExist {
			return nil, errors.New(enum.ErrInvalidToken)
		}
	}

	user, _ := (&model.User{}).FindOne("id = ?", userID)
	if lib.IsNil(user) {
		return nil, errors.New(enum.AuthNotExist)
	}
	return &Claims{
		Id:       userID,
		Username: username,
		Email:    email,
		Token:    tokenKey,
	}, nil
}
