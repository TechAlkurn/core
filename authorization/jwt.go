package authorization

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"app/pkg/enum"

	"app/pkg/application/common/model"
	"app/pkg/core/db/activerecord"

	"github.com/TechAlkurn/core/lib"
	"github.com/golang-jwt/jwt/v5"
)

type JwtService interface {
	VerifyToken(authorization string) (*model.User, error)
	GenerateToken(userId uint32) string
}
type jwtService struct {
	privateKey string
	expiresAt  string
	issure     string
}

func NewJwtService() JwtService {
	privateKey := os.Getenv("SECRET_KEY")
	expiresAt := os.Getenv("TOKEN_MAXAGE")
	return &jwtService{
		privateKey: privateKey,
		expiresAt:  expiresAt,
		issure:     "Alkurn",
	}
}

func (srv *jwtService) _validate(token string) (*jwt.Token, error) {
	if splitToken := strings.Split(token, " "); len(splitToken) == 2 {
		token = splitToken[1]
	}
	return jwt.Parse(token, func(token *jwt.Token) (any, error) {
		if _, isvalid := token.Method.(*jwt.SigningMethodHMAC); !isvalid {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(srv.privateKey), nil
	})
}

// VerifyToken validates the token and returns the user it belongs to. It
// does not store the user anywhere — callers put it in the request context.
func (srv *jwtService) VerifyToken(authorization string) (*model.User, error) {
	token, err := srv._validate(authorization)
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New(enum.AuthRequired)
	}
	if lib.IsEmpty(claims["logged_id"]) || claims["logged_id"] == 0 {
		return nil, errors.New(enum.AuthRequired)
	}

	loginId := lib.ToUint32(claims["logged_id"])
	user, err := (&model.User{}).FindOne("id = ?", loginId)
	if err != nil {
		return nil, errors.New(enum.AuthNotExist)
	}
	/*
		authKey := lib.ToString(claims["auth_key"])
		if user.AuthKey != authKey {
			return errors.New(enum.AuthTokenInvalid)
		}
	*/
	return user, nil
}

type jwtClaim struct {
	LoggedId uint32 `json:"logged_id"`
	AuthKey  string `json:"auth_key"`
	jwt.RegisteredClaims
}

func (srv *jwtService) RefreshToken(userId uint32) (any, error) {
	refreshToken := jwt.New(jwt.SigningMethodHS256)
	rtClaims := refreshToken.Claims.(jwt.MapClaims)
	rtClaims["sub"] = 1
	rtClaims["exp"] = time.Now().Add(time.Hour * 24).Unix()
	rt, err := refreshToken.SignedString([]byte(os.Getenv("SECRET_KEY")))
	if err != nil {
		return nil, err
	}
	return rt, nil
}

func (srv *jwtService) GenerateToken(userId uint32) string {
	authKey := lib.GenerateRandomKey(32)
	refreshToken, err := srv.RefreshToken(userId)
	if err != nil {
		panic(err)
	}
	_ = activerecord.Exec("UPDATE `user` SET `auth_key` = ?, `refresh_token` = ? WHERE `id` = ?", authKey, refreshToken, userId)
	claims := &jwtClaim{
		userId,
		authKey,
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
