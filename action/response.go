package action

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"app/pkg/protos/gen"

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
func (g *Gin) Response(raw *gen.Response) {
	if g.C == nil {
		Log.Error("Nil context in response handler")
		return
	}

	// Check for context cancellation early
	if ctxErr := g.C.Request.Context().Err(); ctxErr != nil {
		Log.Debug("Aborting response due to context error",
			zap.String("path", g.C.FullPath()),
			zap.Error(ctxErr),
		)
		return
	}

	if g.C.Writer.Written() {
		Log.Warn("Attempted duplicate response write", zap.String("path", g.C.FullPath()))
		return
	}

	if g.C.Writer.Status() != http.StatusOK {
		Log.Warn("Failed to write response",
			zap.Int("status", g.C.Writer.Status()),
			zap.String("path", g.C.FullPath()),
		)
		return
	}

	// Create response object
	// Default empty array
	response := BaseResponse{Status: http.StatusOK, Data: []any{}}
	// Handle status code validation
	if status := raw.GetStatus(); status >= 100 && status <= 599 {
		response.Status = int(status)
	} else {
		Log.Warn("Invalid status code received", zap.Int32("proto_status", status), zap.String("path", g.C.FullPath()))
	}
	g.C.Header("Content-Type", "application/json")
	// Safely handle empty responses
	rawData := raw.Data
	if lib.IsNil(rawData) {
		response.Data = []any{} // Set empty array instead of object
		g.C.JSON(http.StatusOK, response)
		return
	}

	// Convert Protobuf Struct to Go map
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
	g.safeJSONWrite(response)
}

// Thread-safe JSON writer with context monitoring
func (g *Gin) safeJSONWrite(response BaseResponse) {
	ctx := g.C.Request.Context()
	// Check if the client is already gone before writing
	if ctx.Err() != nil {
		Log.Warn("Client disconnected before response write",
			zap.String("path", g.C.FullPath()),
			zap.Error(ctx.Err()),
		)
		g.C.Abort()
		return
	}

	// Write response synchronously
	g.C.JSON(http.StatusOK, response)

	// Verify if the write succeeded
	if ctx.Err() != nil {
		Log.Warn("Client disconnected during response write",
			zap.String("path", g.C.FullPath()),
			zap.Error(ctx.Err()),
		)
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

type SafeResponseWriter struct {
	gin.ResponseWriter
	mu     sync.Mutex
	wrote  bool
	status int
}

func (w *SafeResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.wrote {
		return 0, fmt.Errorf("response already written")
	}
	w.wrote = true
	return w.ResponseWriter.Write(data)
}

func (w *SafeResponseWriter) WriteHeader(code int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.wrote {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}

// Middleware to wrap the response writer
func SafeResponseMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		writer := &SafeResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()

		// Finalize status code if not set
		if !writer.wrote && writer.status == 0 {
			writer.WriteHeader(http.StatusOK)
		}
	}
}
