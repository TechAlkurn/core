package lib

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/TechAlkurn/core/cache"
	"github.com/golang-jwt/jwt"
)

var privateKey = []byte(os.Getenv("SECRET_KEY"))

func getToken(bearerToken string) (*jwt.Token, error) {
	strToken := TokenFromRequest(bearerToken)
	token, err := jwt.Parse(strToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return privateKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("error parsing token: %v", err)
	}
	return token, nil
}

func ValidateJWT(str string) error {
	token, err := getToken(str)
	if err != nil {
		return err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if ok && token.Valid {
		if item, ok := claims["id"]; ok {
			SetLoggedUser("id", item)
		}
		return nil
	}
	return errors.New("authentication required")
}

func LoggedUser(str string) (uint32, error) {
	if err := ValidateJWT(str); err != nil {
		return 0, err
	}
	token, _ := getToken(str)
	claims, _ := token.Claims.(jwt.MapClaims)
	if item, ok := claims["id"]; ok {
		return ToUint32(item), nil
	}
	return 0, nil
}

func SetLoggedUser(key string, value any) {
	cache.NewRWMutexCache().Set(key, value)
}

func GetLoggedUser(key string) any {
	if val, ok := cache.NewRWMutexCache().Get(key); ok {
		return val
	}
	return false
}

func GetLoggedId() uint32 {
	if item, ok := cache.NewRWMutexCache().Get("id"); ok {
		return ToUint32(item)
	}
	return 0
}

func LoggedId() uint32 {
	if item, ok := cache.NewRWMutexCache().Get("id"); ok {
		return ToUint32(item)
	}
	return 0
}

func IsOwner(user_id uint32) bool {
	return LoggedId() == user_id
}

func FindAction(str string, controller string) (string, error) {
	action := "public-index"
	if _, err := LoggedUser(str); err == nil {
		action = "index"
	}
	if controller == "authentication" {
		action = "index"
	}
	return action, nil
}

func TokenFromRequest(bearerToken string) (token string) {
	if splitToken := strings.Split(bearerToken, " "); len(splitToken) == 2 {
		token = splitToken[1]
	}
	return token
}

type jwtClaim struct {
	LoggedId uint32 `json:"logged_id"`
	jwt.StandardClaims
}

func JwtGenerate(userId uint32) string {
	claims := &jwtClaim{
		userId,
		jwt.StandardClaims{
			ExpiresAt: time.Now().Add(time.Hour * 24 * 365 * 5).Unix(),
			Issuer:    os.Getenv("SITE_NAME"),
			IssuedAt:  time.Now().Unix(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// encoded the web token
	t, err := token.SignedString([]byte(os.Getenv("SECRET_KEY")))
	if err != nil {
		panic(err)
	}
	return t
}

func GenerateJWT(userId uint32) (string, error) {
	privateKey := []byte(os.Getenv("SECRET_KEY"))
	if len(privateKey) == 0 {
		return "", errors.New("private key is empty")
	}
	tokenTTL, _ := strconv.Atoi(os.Getenv("TOKEN_TTL"))
	sapi := os.Getenv("API_ENDPOINT")
	claimsParams := jwt.MapClaims{
		"id":  userId,
		"iss": sapi,
		"aud": sapi,
		"iat": time.Now().Unix(),
		"nbf": time.Now().Unix(),
		"eat": time.Now().Add(time.Second * time.Duration(tokenTTL)).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claimsParams).SignedString(privateKey)
}

func FindHostName() string {
	name, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	return name
}
