package lib

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	mrand "math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

/*
Security Encryption Bytes (key)
SecurityEncryptionBytes := lib.SecurityEncryptionKey()
*/
var SecurityEncryptionBytes = []byte{35, 213, 66, 145, 241, 141, 199, 3, 87, 98, 128, 181, 22, 153, 174, 99, 53, 27, 214, 30, 69, 85, 36, 3, 211, 91, 136, 101, 201, 187, 81, 26}

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
	if token == "" {
		return false
	}
	timestamp := strings.Split(string(token), "_")
	rawTime, err := strconv.ParseUint(timestamp[1], 10, 64)
	if err != nil {
		return false
	}
	return int64(rawTime)+7200 >= time.Now().Unix()
}

func SecurityEncryptionKey() (key []byte) {
	salt := make([]byte, 16) // Generate a random salt
	_, err := rand.Read(salt)
	if err != nil {
		fmt.Println("Error generating salt:", err)
		return
	}
	key = pbkdf2.Key([]byte(os.Getenv("ENCRYPTION_KEY")), make([]byte, 16), 1000, 32, sha256.New)
	return
}

func SecurityEncrypt(str string) (value string) {
	return
}

func SecurityDecrypt(str string) any {
	if !strings.Contains(str, "::") {
		return nil
	}
	result := strings.Split(str, "::")
	value, _ := Decrypt([]byte(result[1]))
	return value
}

/*
	key := lib.SecurityEncryptionKey() //(SecurityEncryptionBytes)
	log.Println(key)
	EncryptedTextDemo, _ := lib.Encrypt([]byte("Hello, World!"))
	log.Println(string(EncryptedTextDemo))
	DecryptTextDemo, _ := lib.Decrypt(EncryptedTextDemo)
	log.Println(string(DecryptTextDemo))
	os.Exit(0)
*/

// Encrypt method is to encrypt or hide any classified text
func Encrypt(plainText []byte) ([]byte, error) {
	block, err := aes.NewCipher(SecurityEncryptionBytes)
	if err != nil {
		return nil, err
	}
	cipherText := make([]byte, aes.BlockSize+len(plainText))
	iv := cipherText[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(cipherText[aes.BlockSize:], plainText)
	return cipherText, nil
}

// Decrypt method is to extract back the encrypted text
func Decrypt(cipherText []byte) ([]byte, error) {
	block, err := aes.NewCipher(SecurityEncryptionBytes)
	if err != nil {
		return nil, err
	}
	if len(cipherText) < aes.BlockSize {
		return nil, fmt.Errorf("text too short")
	}
	iv := cipherText[:aes.BlockSize]
	cipherText = cipherText[aes.BlockSize:]
	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(cipherText, cipherText)
	return cipherText, nil
}
