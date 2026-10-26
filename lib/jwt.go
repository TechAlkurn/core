package lib

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	mu        sync.Mutex
	muStorage = make(map[string]any)
)

// SECRET_KEY is read on every call rather than at package init, so a .env
// loaded in main() is picked up.
func secretKey() ([]byte, error) {
	key := []byte(os.Getenv("SECRET_KEY"))
	if len(key) == 0 {
		return nil, errors.New("private key is empty")
	}
	return key, nil
}

// claimUserID reads the user id, falling back to the "logged_id" claim
// written by older JwtGenerate tokens.
func claimUserID(claims jwt.MapClaims) (any, bool) {
	if item, ok := claims["id"]; ok {
		return item, true
	}
	item, ok := claims["logged_id"]
	return item, ok
}

func getToken(bearerToken string) (*jwt.Token, error) {
	key, err := secretKey()
	if err != nil {
		return nil, err
	}
	strToken := TokenFromRequest(bearerToken)
	token, err := jwt.Parse(strToken, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return key, nil
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
		if item, ok := claimUserID(claims); ok {
			SetMuStorage("id", item)
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
	if item, ok := claimUserID(claims); ok {
		return ToUint32(item), nil
	}
	return 0, nil
}

func SetLoggedUser(key string, value any) {
	SetMuStorage(key, value)
}

func UnSetLoggedUser() {
	mu.Lock()
	defer mu.Unlock()
	delete(muStorage, "id")
}

func Has(key string) bool {
	mu.Lock()
	defer mu.Unlock()
	_, ok := muStorage[key]
	return ok
}

func Clear() {
	mu.Lock()
	defer mu.Unlock()
	muStorage = make(map[string]any)
}

func SetMuStorage(key string, value any) {
	mu.Lock()
	defer mu.Unlock()
	muStorage[key] = value
}

func GetMuStorage(key string) any {
	mu.Lock()
	defer mu.Unlock()
	if val, ok := muStorage[key]; ok {
		return val
	}
	return nil
}

func GetLoggedId() uint32 {
	val := GetMuStorage("id")
	if !IsNil(val) || !IsEmpty(val) {
		return ToUint32(val)
	}
	return 0
}

func LoggedId() uint32 {
	return GetLoggedId()
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
	LoggedId uint32 `json:"id"`
	jwt.RegisteredClaims
}

func JwtGenerate(userId uint32) string {
	claims := &jwtClaim{
		userId,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24 * 365 * 5)),
			Issuer:    os.Getenv("SITE_NAME"),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
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
	privateKey, err := secretKey()
	if err != nil {
		return "", err
	}
	tokenTTL, _ := strconv.Atoi(os.Getenv("TOKEN_TTL"))
	sapi := os.Getenv("API_ENDPOINT")
	now := time.Now()
	claimsParams := jwt.MapClaims{
		"id":  userId,
		"iss": sapi,
		"aud": sapi,
		"iat": now.Unix(),
		"nbf": now.Unix(),
	}
	// TOKEN_TTL is in seconds; unset or 0 means the token does not expire.
	if tokenTTL > 0 {
		claimsParams["exp"] = now.Add(time.Second * time.Duration(tokenTTL)).Unix()
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
