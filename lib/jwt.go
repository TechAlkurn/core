package lib

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt"
)

var (
	loggedUser = make(map[string]any)
	mu         sync.Mutex // Mutex for concurrent access to the map
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
		userId := claims["id"].(float64)
		SetLoggedUser("id", uint32(userId))
		return nil
	}
	return errors.New("authentication required")
}

func LoggedUser(str string) (uint32, error) {
	err := ValidateJWT(str)
	if err != nil {
		return 0, err
	}
	token, _ := getToken(str)
	claims, _ := token.Claims.(jwt.MapClaims)
	userId := uint32(claims["id"].(float64))
	return userId, nil
}

func SetLoggedUser(key string, value any) {
	mu.Lock()
	defer mu.Unlock()
	loggedUser[key] = value
}

func GetLoggedUser(key string) any {
	mu.Lock()
	defer mu.Unlock()
	return loggedUser[key]
}

func GetLoggedId() uint32 {
	user_id, _ := strconv.ParseUint(fmt.Sprintf("%v", GetLoggedUser("id")), 10, 64)
	return uint32(user_id)
}

func LoggedId() uint32 {
	user_id, _ := strconv.ParseUint(fmt.Sprintf("%v", GetLoggedUser("id")), 10, 64)
	return uint32(user_id)
}

func IsOwner(user_id uint32) bool {
	return LoggedId() == user_id
}

func FindAction(str string, controller string) (string, error) {
	_, err := LoggedUser(str)
	action := "public-index"
	if err == nil {
		action = "index"
	}
	if controller == "authentication" {
		action = "index"
	}
	return action, nil
}

func TokenFromRequest(bearerToken string) string {
	splitToken := strings.Split(bearerToken, " ")
	if len(splitToken) == 2 {
		return splitToken[1]
	}
	return ""
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
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsParams)
	return token.SignedString(privateKey)
}

func FindHostName() string {
	name, err := os.Hostname()
	if err != nil {
		panic(err)
	}
	return name
}
