// Package services provides business logic services
package services

import (
	"sync"
	"time"

	"gpt-load/internal/types"

	"github.com/sirupsen/logrus"
)

// LoginLimiter manages global login attempt limiting to prevent brute force attacks
type LoginLimiter struct {
	configManager      types.ConfigManager
	failedAttempts     int
	lockoutUntil       time.Time
	mutex              sync.RWMutex
}

// NewLoginLimiter creates a new login limiter
func NewLoginLimiter(configManager types.ConfigManager) *LoginLimiter {
	return &LoginLimiter{
		configManager: configManager,
	}
}

// CheckLogin checks if login is allowed and returns remaining lockout time if locked
func (ll *LoginLimiter) CheckLogin() (bool, time.Duration) {
	ll.mutex.RLock()
	defer ll.mutex.RUnlock()

	if ll.lockoutUntil.After(time.Now()) {
		remaining := time.Until(ll.lockoutUntil)
		return false, remaining
	}

	return true, 0
}

// RecordSuccess records a successful login and resets the failed attempt counter
func (ll *LoginLimiter) RecordSuccess() {
	ll.mutex.Lock()
	defer ll.mutex.Unlock()

	ll.failedAttempts = 0
	ll.lockoutUntil = time.Time{}
	logrus.Debug("Login successful, failed attempts counter reset")
}

// RecordFailure records a failed login attempt and locks if threshold reached
func (ll *LoginLimiter) RecordFailure() (bool, time.Duration) {
	ll.mutex.Lock()
	defer ll.mutex.Unlock()

	authConfig := ll.configManager.GetAuthConfig()
	ll.failedAttempts++
	logrus.Debugf("Login failed, attempt count: %d/%d", ll.failedAttempts, authConfig.MaxFailedAttempts)

	// Check if threshold reached
	if ll.failedAttempts >= authConfig.MaxFailedAttempts {
		ll.lockoutUntil = time.Now().Add(time.Duration(authConfig.LockoutDuration) * time.Second)
		duration := time.Duration(authConfig.LockoutDuration) * time.Second
		logrus.Warnf("Login locked due to %d failed attempts. Locked for %v", ll.failedAttempts, duration)
		return true, duration
	}

	return false, 0
}

// Reset clears the failed attempts counter (for admin use)
func (ll *LoginLimiter) Reset() {
	ll.mutex.Lock()
	defer ll.mutex.Unlock()

	ll.failedAttempts = 0
	ll.lockoutUntil = time.Time{}
	logrus.Info("Login limiter reset by admin")
}

// GetStatus returns current failed attempts and lockout status
func (ll *LoginLimiter) GetStatus() (int, time.Time) {
	ll.mutex.RLock()
	defer ll.mutex.RUnlock()

	return ll.failedAttempts, ll.lockoutUntil
}
