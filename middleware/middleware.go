package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gofiber/fiber/v2"
)

// ShutdownBackgroundRefresh stops the background refresh routine.
func (v *LicenseClient) ShutdownBackgroundRefresh() {
	v.bgConfig.mu.Lock()
	defer v.bgConfig.mu.Unlock()

	if !v.bgConfig.started {
		return
	}

	// Cancel the context to signal goroutines to stop
	if v.bgConfig.cancel != nil {
		v.bgConfig.cancel()
		v.bgConfig.cancel = nil
		log.Println("[license-sdk] Background license validation stopped")
	}
	v.bgConfig.started = false
}

// startBackgroundRefreshOnce ensures background refresh is only started once
// across all middleware instances.
var (
	bgRefreshStarted = false
	bgRefreshMutex   sync.Mutex
)

func (v *LicenseClient) startBackgroundRefreshOnce() {
	bgRefreshMutex.Lock()
	defer bgRefreshMutex.Unlock()

	if !bgRefreshStarted {
		bgRefreshStarted = true
		v.StartBackgroundRefresh(context.Background())
	}
}

// Middleware returns a Fiber middleware that validates licenses.
// This middleware will automatically start background license validation
// exactly once across all instances.
func (v *LicenseClient) Middleware() fiber.Handler {
	v.startBackgroundRefreshOnce()

	return func(c *fiber.Ctx) error {
		// Create a child context with timeout for validation
		ctx, cancel := context.WithTimeout(c.Context(), v.cli.Timeout)
		defer cancel()

		res, err := v.Validate(ctx)
		if err != nil {
			log.Printf("[license-sdk] License validation failed: %s", err.Error())
			return c.Status(http.StatusUnauthorized).JSON(fiber.Map{
				"error": "License validation failed",
			})
		}

		if !res.Valid {
			log.Printf("[license-sdk] Invalid license detected (expires in %d days)", res.ExpiryDaysLeft)
			return c.Status(http.StatusForbidden).JSON(fiber.Map{
				"error": "Invalid license",
			})
		}

		// Propagate expiration information to response headers
		c.Set("X-License-Expiry-Days", fmt.Sprintf("%d", res.ExpiryDaysLeft))

		// Continue to the next handler
		return c.Next()
	}
}
