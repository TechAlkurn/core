package action

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/TechAlkurn/core/lib"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

var Log = logrus.New()

type Gin struct {
	C *gin.Context
}

func NewResponse(c *gin.Context) *Gin {
	return &Gin{C: c}
}

type BaseResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message,omitempty"`
	Token   string `json:"token,omitempty"`
	Data    any    `json:"data"`
}

// Response setting gin.JSON: Pending:-> work on Response
func (g *Gin) Response(rawData []byte) {
	if g.C == nil {
		Log.Error("Nil context in response handler")
		return
	}
	// Check if context is already canceled
	select {
	case <-g.C.Request.Context().Done():
		Log.Warn("Request context canceled before response",
			zap.String("path", g.C.FullPath()),
			zap.Error(g.C.Request.Context().Err()),
		)
		return
	default:
	}

	// Early return if writer is already closed
	if g.C.Writer.Written() {
		Log.Warn("Attempted write to closed writer",
			zap.String("path", g.C.FullPath()),
		)
		return
	}
	// Create response object
	response := BaseResponse{
		Status: http.StatusOK,
		Data:   []any{}, // Default empty array
	}

	// Safely handle empty responses
	if len(rawData) == 0 {
		response.Data = []any{} // Set empty array instead of object
		g.C.JSON(http.StatusOK, response)
		return
	}

	// Handle empty responses
	if len(rawData) > 0 {
		var parsedData any
		if err := json.Unmarshal(rawData, &parsedData); err != nil {
			Log.Warn("Failed to unmarshal response data",
				zap.Error(err),
				zap.ByteString("raw", rawData),
			)
			response.Data = json.RawMessage(rawData)
		} else {
			if m, ok := parsedData.(map[string]any); ok {
				// Extract special fields
				if token, exists := m["token"].(string); exists {
					response.Token = token
					delete(m, "token")
				}
				if msg, exists := m["message"].(string); exists {
					response.Message = msg
					delete(m, "message")
				}
				response.Data = m
			} else {
				response.Data = parsedData
			}
		}
	}

	// Safe write with context monitoring
	g.safeJSONWrite(response)
}

// Thread-safe JSON writer with context monitoring
func (g *Gin) safeJSONWrite(response BaseResponse) {
	// Create context-aware writer
	ctx := g.C.Request.Context()
	// Use a channel to capture write completion
	done := make(chan struct{})

	go func() {
		defer close(done)
		g.C.JSON(http.StatusOK, response)
	}()

	select {
	case <-done:
		// Response written successfully
		if g.C.Writer.Status() != http.StatusOK {
			Log.Warn("Failed to write response",
				zap.Int("status", g.C.Writer.Status()),
				zap.String("path", g.C.FullPath()),
			)
		}
	case <-ctx.Done():
		Log.Warn("Client disconnected during response write",
			zap.String("path", g.C.FullPath()),
			zap.Error(ctx.Err()),
		)
		// Abort any ongoing processing
		g.C.Abort()
	}
}

func (g *Gin) Abort(err error) {
	if g.C == nil {
		Log.Error("Abort called with nil context")
		return
	}
	Log.Warn("Request aborted",
		zap.Error(err),
		zap.String("path", g.C.FullPath()),
	)

	g.C.JSON(http.StatusBadRequest, BaseResponse{
		Status:  http.StatusBadRequest,
		Message: g.cleanErrorMessage(err),
	})
	g.C.Abort()
}

func (g *Gin) Failed(code int, err error) {
	if g.C == nil {
		Log.Error("Failed called with nil context")
		return
	}
	Log.Warn("Request failed",
		zap.Int("code", code),
		zap.Error(err),
		zap.String("path", g.C.FullPath()),
	)

	g.C.JSON(code, BaseResponse{
		Status:  code,
		Message: g.cleanErrorMessage(err),
	})
	g.C.Abort()
}

func (g *Gin) cleanErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	// Extract meaningful error message
	msg := err.Error()
	if idx := strings.Index(msg, "desc = "); idx != -1 {
		msg = msg[idx+len("desc = "):]
	}
	// Sanitize sensitive information
	return lib.ToString(msg)
}
