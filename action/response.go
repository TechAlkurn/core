package action

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"app/pkg/protos/gen"

	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
)

type Gin struct {
	C *gin.Context
}

func NewResponse(c *gin.Context) *Gin {
	return &Gin{C: c}
}

type J struct {
	Mode    bool   `json:"mode"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

type T struct {
	*J
	Token string      `json:"token"`
	Data  interface{} `json:"data"`
}

type R struct {
	*J
	Data interface{} `json:"data"`
}

// Response setting gin.JSON: Pending:-> work on Response

func (g *Gin) Response(raw *gen.Response) {
	var res interface{}
	err := json.Unmarshal(raw.Data, &res)
	if err != nil {
		g.Abort(err)
		return
	}
	param := J{Mode: false, Status: http.StatusOK}
	var req interface{}
	switch d := res.(type) {
	case map[string]interface{}:
		message := g._message(d)
		if !lib.Empty(message) {
			param.Message = message
		}
		if token, ok := d["token"].(string); ok && !lib.Empty(token) {
			delete(d, "token")
			req = &T{J: &param, Token: token, Data: d}
		} else {
			req = &R{J: &param, Data: d}
		}
	case []interface{}:
		// Handle each element of the slice separately
		var dataArray []interface{}
		dataArray = append(dataArray, d...)
		req = &R{J: &param, Data: dataArray}
	default:
		req = &R{J: &param, Data: d}
	}

	if req == nil {
		// Handle the case when req is nil
		g.Abort(errors.New("invalid response data"))
		return
	}

	// Check if g.C is not nil before using it
	if g.C != nil {
		g.C.JSON(http.StatusOK, req)
	} else {
		// Handle the case when g.C is nil
		g.Abort(errors.New("gin.Context is nil"))
		return
	}
}

func (g *Gin) _message(response map[string]interface{}) string {
	message := ""
	if response["message"] != nil {
		message = response["message"].(string)
		delete(response, "message")
	}
	return message
}

func (g *Gin) _extract(err error) (message string) {
	index := strings.Index(err.Error(), "desc = ")
	message = err.Error()
	if index != -1 {
		// Extract the description part
		message = err.Error()[index+len("desc = "):]
	}
	return message
}

func (g *Gin) Abort(err error) {
	g.C.JSON(http.StatusBadRequest, &J{
		Mode:    false,
		Status:  http.StatusBadRequest,
		Message: g._extract(err),
	})
	g.C.Abort()
}

func (g *Gin) Failed(code int, err error) {
	g.C.JSON(code, &J{
		Mode:    false,
		Status:  code,
		Message: g._extract(err),
	})
	g.C.Abort()
}

func Response(data any) (*gen.Response, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &gen.Response{Data: raw}, nil
}
