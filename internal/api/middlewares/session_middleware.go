package middlewares

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	SessionCookieName = "papoql_session"
	SessionCookieAge  = 3600 // 1 hour in seconds
	SessionContextKey = "session_id"
	SessionHeaderName = "X-Session-ID"
)

// SessionMiddleware resolves the session ID with the following priority:
//  1. Cookie "papoql_session"   — browser clients
//  2. Header "X-Session-ID"     — CLI / non-browser clients
//  3. Generate a new UUID       — first-time clients
//
// The resolved ID is always echoed back in both the cookie and the
// "X-Session-ID" response header so every type of client can persist it.
func SessionMiddleware() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		sessionID, err := ctx.Cookie(SessionCookieName)
		if err != nil || sessionID == "" {
			sessionID = ctx.GetHeader(SessionHeaderName)
		}
		if sessionID == "" {
			sessionID = uuid.NewString()
		}

		// Echo back so the client can persist it however it prefers
		ctx.SetCookie(
			SessionCookieName,
			sessionID,
			SessionCookieAge,
			"/",
			"",
			false, // set true behind HTTPS
			true,  // HttpOnly
		)
		ctx.Header(SessionHeaderName, sessionID)

		ctx.Set(SessionContextKey, sessionID)
		ctx.Next()
	}
}
