package lib

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/cons"
)

func InstagramToken(query string) (data []byte, error error) {
	req := action.Request(query)
	code := req.Query("code")
	redirect_uri := req.Query("redirect_uri")
	if code == "" {
		return nil, errors.New(cons.AuthorizationCodeRequired)
	}
	payload := &bytes.Buffer{}
	writer := multipart.NewWriter(payload)
	_ = writer.WriteField("client_id", "2027987457677485")
	_ = writer.WriteField("client_secret", "41575084783a1f5217b048089e646918")
	_ = writer.WriteField("grant_type", "authorization_code")
	_ = writer.WriteField("redirect_uri", redirect_uri)
	_ = writer.WriteField("code", code)
	err := writer.Close()
	if err != nil {
		return nil, err
		// fmt.Println(err)
	}
	client := &http.Client{}
	r, err := http.NewRequest("POST", "https://api.instagram.com/oauth/access_token", payload)
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}
