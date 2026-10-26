package lib

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

// legacyEncryptionKey is the hardcoded key older releases used for AES-CFB.
// It is only used to decrypt ciphertext produced by those releases; new data
// is always encrypted with the ENCRYPTION_KEY-derived key.
var legacyEncryptionKey = []byte{35, 213, 66, 145, 241, 141, 199, 3, 87, 98, 128, 181, 22, 153, 174, 99, 53, 27, 214, 30, 69, 85, 36, 3, 211, 91, 136, 101, 201, 187, 81, 26}

var letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateRandomKey(length int32) string {
	b := make([]byte, length)
	// rand.Read(b)
	for i := range b {
		b[i] = letterBytes[mrand.Int63()%int64(len(letterBytes))]
	}
	// return string(b)
	encodedKey := Encode(b)
	return string(encodedKey[0:length])
}

func GenerateOtp(low int, hi int) int {
	return low + mrand.Intn(hi-low)
}

func GenerateHashedPassword(password string) (string, error) {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("Error", err)
	}
	return string(hashedPassword), err
}

func VerifyPassword(hashedPassword string, candidatePassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(candidatePassword))
}

func Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

func Decode(s string) []byte {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return data
}

func GenerateToken(ttl time.Duration, payload any, secretJWTKey string) (string, error) {
	token := jwt.New(jwt.SigningMethodHS256)
	now := time.Now().UTC()

	claims := token.Claims.(jwt.MapClaims)
	claims["sub"] = payload
	claims["exp"] = now.Add(ttl).Unix()
	claims["iat"] = now.Unix()
	claims["nbf"] = now.Unix()

	tokenString, err := token.SignedString([]byte(secretJWTKey))
	if err != nil {
		return "", fmt.Errorf("generating JWT Token failed: %w", err)
	}

	return tokenString, nil
}

func ValidateToken(token string, signedJWTKey string) (any, error) {
	tok, err := jwt.Parse(token, func(jwtToken *jwt.Token) (any, error) {
		if _, ok := jwtToken.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected method: %s", jwtToken.Header["alg"])
		}

		return []byte(signedJWTKey), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalidate token: %w", err)
	}

	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return nil, fmt.Errorf("invalid token claim")
	}

	return claims["sub"], nil
}

func IsPasswordResetTokenValid(token string) bool {
	if IsEmpty(token) {
		return false
	}
	timestamp := strings.Split(string(token), "_")
	rawTime, err := strconv.ParseUint(timestamp[1], 10, 64)
	if err != nil {
		return false
	}
	return int64(rawTime)+7200 >= time.Now().Unix()
}

// SecurityEncryptionKey derives the 32-byte AES key from ENCRYPTION_KEY.
// The salt is fixed so the same key is derived on every call. Returns nil
// when ENCRYPTION_KEY is unset.
func SecurityEncryptionKey() (key []byte) {
	secret := os.Getenv("ENCRYPTION_KEY")
	if secret == "" {
		return nil
	}
	return pbkdf2.Key([]byte(secret), make([]byte, 16), 1000, 32, sha256.New)
}

// SecurityEncrypt returns "v1::" followed by the base64 ciphertext, the format
// SecurityDecrypt reads. Returns "" if encryption fails.
func SecurityEncrypt(str string) (value string) {
	cipherText, err := Encrypt([]byte(str))
	if err != nil {
		return
	}
	return "v1::" + base64.StdEncoding.EncodeToString(cipherText)
}

func SecurityDecrypt(str string) any {
	if !strings.Contains(str, "::") {
		return nil
	}
	result := strings.SplitN(str, "::", 2)
	cipherText, err := base64.StdEncoding.DecodeString(result[1])
	if err != nil {
		// Older values stored the raw ciphertext bytes.
		cipherText = []byte(result[1])
	}
	value, _ := Decrypt(cipherText)
	return value
}

// Encrypt method is to encrypt or hide any classified text.
// Uses AES-256-GCM with the ENCRYPTION_KEY-derived key; output is nonce||ciphertext.
func Encrypt(plainText []byte) ([]byte, error) {
	key := SecurityEncryptionKey()
	if key == nil {
		return nil, errors.New("ENCRYPTION_KEY is not set")
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plainText, nil), nil
}

// Decrypt method is to extract back the encrypted text.
// Ciphertext that fails GCM authentication is retried as legacy AES-CFB
// with the old hardcoded key.
func Decrypt(cipherText []byte) ([]byte, error) {
	if key := SecurityEncryptionKey(); key != nil {
		gcm, err := newGCM(key)
		if err != nil {
			return nil, err
		}
		if len(cipherText) >= gcm.NonceSize() {
			nonce, sealed := cipherText[:gcm.NonceSize()], cipherText[gcm.NonceSize():]
			if plainText, err := gcm.Open(nil, nonce, sealed, nil); err == nil {
				return plainText, nil
			}
		}
	}
	return decryptLegacy(cipherText)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func decryptLegacy(cipherText []byte) ([]byte, error) {
	block, err := aes.NewCipher(legacyEncryptionKey)
	if err != nil {
		return nil, err
	}
	if len(cipherText) < aes.BlockSize {
		return nil, fmt.Errorf("text too short")
	}
	plainText := make([]byte, len(cipherText)-aes.BlockSize)
	stream := cipher.NewCFBDecrypter(block, cipherText[:aes.BlockSize])
	stream.XORKeyStream(plainText, cipherText[aes.BlockSize:])
	return plainText, nil
}
