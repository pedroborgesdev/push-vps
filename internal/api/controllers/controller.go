package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/pedroborgesdev/papoql/internal/api/dto"
	"github.com/pedroborgesdev/papoql/internal/api/logger"
	"github.com/pedroborgesdev/papoql/internal/api/middlewares"
	"github.com/pedroborgesdev/papoql/internal/api/services"
	"github.com/pedroborgesdev/papoql/internal/api/session"
	"github.com/pedroborgesdev/papoql/internal/api/utils"

	"github.com/gin-gonic/gin"
)

type Controller struct {
	service        *services.Service
	sessions       *session.Store
	activePrompts  map[string]activePrompt
	activePromptsM sync.Mutex
	requestSeq     uint64
}

type activePrompt struct {
	cancel    context.CancelFunc
	sessionID string
}

func NewController() *Controller {
	return &Controller{
		service:       services.NewService(),
		sessions:      session.NewStore(),
		activePrompts: make(map[string]activePrompt),
	}
}

func (c *Controller) nextRequestID() string {
	id := atomic.AddUint64(&c.requestSeq, 1)
	return fmt.Sprintf("req-%d", id)
}

func (c *Controller) registerActivePrompt(requestID string, cancel context.CancelFunc, sessionID string) {
	c.activePromptsM.Lock()
	defer c.activePromptsM.Unlock()
	c.activePrompts[requestID] = activePrompt{cancel: cancel, sessionID: sessionID}
}

func (c *Controller) unregisterActivePrompt(requestID string) {
	c.activePromptsM.Lock()
	defer c.activePromptsM.Unlock()
	delete(c.activePrompts, requestID)
}

func (c *Controller) cancelActivePrompt(requestID string, sessionID string) bool {
	c.activePromptsM.Lock()
	entry, ok := c.activePrompts[requestID]
	if ok {
		if entry.sessionID != sessionID {
			c.activePromptsM.Unlock()
			return false
		}
		delete(c.activePrompts, requestID)
	}
	c.activePromptsM.Unlock()

	if !ok {
		return false
	}

	entry.cancel()
	return true
}

func (c *Controller) Prompt(ctx *gin.Context) {
	var req dto.Prompt
	if err := ctx.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	logPrompt := req.Prompt
	logger.Infof("Prompt received", []logger.ParamPair{{Key: "prompt", Value: truncateString(logPrompt, 50)}})

	sessionID, _ := ctx.Get(middlewares.SessionContextKey)
	history := c.sessions.GetHistory(sessionID.(string))

	steps, sqlQuery, currentContextMB, maxContextMB, err := c.service.Prompt(ctx.Request.Context(), req.Prompt, history)
	if err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		logger.Errorf("Response not resolved", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return
	}

	c.sessions.Save(sessionID.(string), req.Prompt, steps)

	utils.Success(ctx, gin.H{
		"prompt":                req.Prompt,
		"response":              steps,
		"sql":                   sqlQuery,
		"current_context_usage": currentContextMB,
		"max_context_usage":     maxContextMB,
		"max_request_size_kb":   middlewares.MaxRequestBodyBytes / 1024,
	})
}

func (c *Controller) PromptStream(ctx *gin.Context) {
	var req dto.Prompt
	if err := ctx.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	ctx.Header("Content-Type", "text/event-stream")
	ctx.Header("Cache-Control", "no-cache")
	ctx.Header("Connection", "keep-alive")

	flusher, ok := ctx.Writer.(http.Flusher)
	if !ok {
		utils.BadRequest(ctx, gin.H{"error": "streaming not supported"})
		return
	}

	sendEvent := func(event string, data interface{}) {
		payload, _ := json.Marshal(gin.H{"event": event, "data": data})
		fmt.Fprintf(ctx.Writer, "data: %s\n\n", string(payload))
		flusher.Flush()
	}

	logPrompt := req.Prompt
	logger.Infof("Prompt stream received", []logger.ParamPair{{Key: "prompt", Value: truncateString(logPrompt, 50)}})

	sessionID, _ := ctx.Get(middlewares.SessionContextKey)
	sid := sessionID.(string)
	history := c.sessions.GetHistory(sid)

	requestID := c.nextRequestID()
	promptCtx, cancelPrompt := context.WithCancel(ctx.Request.Context())
	c.registerActivePrompt(requestID, cancelPrompt, sid)
	defer c.unregisterActivePrompt(requestID)
	defer cancelPrompt()

	sendEvent("status", gin.H{"message": "starting processing", "request_id": requestID})

	steps, sqlQuery, currentContextMB, maxContextMB, err := c.service.Prompt(promptCtx, req.Prompt, history, func(stage string, message string) {
		sendEvent("thought", gin.H{"stage": stage, "message": message})
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			sendEvent("canceled", gin.H{"request_id": requestID, "message": "processing canceled"})
			logger.Infof("Prompt stream canceled", []logger.ParamPair{{Key: "request_id", Value: requestID}})
			return
		}
		sendEvent("error", gin.H{"error": err.Error()})
		logger.Errorf("Stream response not resolved", []logger.ParamPair{{Key: "error", Value: err.Error()}})
		return
	}

	c.sessions.Save(sid, req.Prompt, steps)

	sendEvent("result", gin.H{
		"prompt":                req.Prompt,
		"response":              steps,
		"sql":                   sqlQuery,
		"current_context_usage": currentContextMB,
		"max_context_usage":     maxContextMB,
		"max_request_size_kb":   middlewares.MaxRequestBodyBytes / 1024,
	})
	sendEvent("done", gin.H{"ok": true})
}

func (c *Controller) PromptCancel(ctx *gin.Context) {
	var req dto.PromptCancel
	if err := ctx.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	sessionID, _ := ctx.Get(middlewares.SessionContextKey)
	sid := sessionID.(string)

	if ok := c.cancelActivePrompt(req.RequestID, sid); !ok {
		utils.BadRequest(ctx, gin.H{"error": "request not found or already finished"})
		return
	}

	logger.Infof("Prompt cancellation requested", []logger.ParamPair{{Key: "request_id", Value: req.RequestID}})
	utils.Success(ctx, gin.H{"canceled": true, "request_id": req.RequestID})
}

func (c *Controller) ClearContext(ctx *gin.Context) {
	sessionID, _ := ctx.Get(middlewares.SessionContextKey)
	c.sessions.ClearContext(sessionID.(string))
	logger.Infof("Context cleared", []logger.ParamPair{{Key: "session", Value: sessionID}})
	utils.Success(ctx, gin.H{"cleared": true})
}

func (c *Controller) ModelGET(ctx *gin.Context) {
	current, models, err := c.service.ModelGET()
	if err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	utils.Success(ctx, gin.H{
		"current": current,
		"models":  models,
	})
}

func (c *Controller) ModelPUT(ctx *gin.Context) {
	var req dto.Models
	if err := ctx.ShouldBindJSON(&req); err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	err := c.service.ModelPUT(ctx, req.Model)
	if err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		logger.Errorf("Model hasn't changed", []logger.ParamPair{{Key: "error", Value: err.Error()}, {Key: "model", Value: req.Model}})
		return
	}

	utils.Success(ctx, gin.H{
		"model": req.Model,
	})
	logger.Infof("Model has been changed", []logger.ParamPair{{Key: "model", Value: req.Model}})
}

func (c *Controller) SchemaPOST(ctx *gin.Context) {
	schema, err := c.service.SchemaPOST()
	if err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	utils.Success(ctx, gin.H{
		"schema": schema,
	})
}

func (c *Controller) SchemaGET(ctx *gin.Context) {
	schema, err := c.service.SchemaGET()
	if err != nil {
		utils.BadRequest(ctx, gin.H{"error": err.Error()})
		return
	}

	utils.Success(ctx, gin.H{
		"schema": schema,
	})
}

func truncateString(input string, maxLength int) string {
	if len(input) > maxLength {
		return input[:maxLength] + "..."
	}
	return input
}
